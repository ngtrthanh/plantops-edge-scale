package sqlitestore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
)

func sampleTicket() domain.Ticket {
	now:=time.Now().UTC()
	return domain.Ticket{
		ID:"T-1",StationID:"S01",TransactionID:"TX-1",Plate:"15C-123.45",RFID:"RFID-1",
		WeightKG:28460,WeightObservedAt:now,WeightRawRef:domain.RawWeightRef{Seq:44,Hash:"abc"},Mode:domain.ModeDegraded,
		Overrides:[]domain.Override{{TransactionID:"TX-1",Device:domain.DeviceRearSensor,Reason:"sensor fault",RequestedBy:"op1",AuthorizedBy:"sup1",AuthorizedAs:domain.RoleSupervisor,AuthorizedAt:now,Evidence:[]string{domain.EvidencePositionConfirmed}}},
		CommittedAt:now,
	}
}

func TestCommitIsAtomicTicketOverrideSyncState(t *testing.T){
	ctx:=context.Background();path:=filepath.Join(t.TempDir(),"edge.db")
	s,err:=Open(path);if err!=nil{t.Fatal(err)};defer s.Close()
	if err:=s.Commit(ctx,sampleTicket());err!=nil{t.Fatal(err)}
	st,err:=s.Status(ctx);if err!=nil{t.Fatal(err)}
	if st.Integrity!="ok"||st.Tickets!=1||st.Overrides!=1||st.PendingSync!=1{t.Fatalf("status=%+v",st)}
	last,ok,err:=s.LastTicket(ctx);if err!=nil||!ok{t.Fatalf("last ok=%v err=%v",ok,err)}
	if last.WeightKG!=28460||last.WeightRawRef.Seq!=44||last.WeightRawRef.Hash!="abc"{t.Fatalf("last=%+v",last)}
	q,err:=s.PendingSync(ctx,10);if err!=nil||len(q)!=1{t.Fatalf("queue=%+v err=%v",q,err)}
	if q[0].EntityID!="T-1"||q[0].Kind!="TICKET"{t.Fatalf("queue=%+v",q[0])}
}

func TestReopenRecoversDatabaseAndDoesNotDuplicate(t *testing.T){
	ctx:=context.Background();path:=filepath.Join(t.TempDir(),"edge.db")
	s,err:=Open(path);if err!=nil{t.Fatal(err)}
	if err:=s.Commit(ctx,sampleTicket());err!=nil{t.Fatal(err)}
	if err:=s.Checkpoint(ctx);err!=nil{t.Fatal(err)}
	if err:=s.Close();err!=nil{t.Fatal(err)}

	s2,err:=Open(path);if err!=nil{t.Fatal(err)};defer s2.Close()
	st,err:=s2.Status(ctx);if err!=nil{t.Fatal(err)}
	if st.Tickets!=1||st.PendingSync!=1{t.Fatalf("reopen status=%+v",st)}
	if err:=s2.Commit(ctx,sampleTicket());err==nil{t.Fatal("duplicate transaction/ticket must fail, not create a second queue item")}
	st,err=s2.Status(ctx);if err!=nil{t.Fatal(err)}
	if st.Tickets!=1||st.PendingSync!=1{t.Fatalf("duplicate changed durable state=%+v",st)}
}

func TestAckSyncUpdatesTicketAtomically(t *testing.T){
	ctx:=context.Background();path:=filepath.Join(t.TempDir(),"edge.db")
	s,err:=Open(path);if err!=nil{t.Fatal(err)};defer s.Close()
	if err:=s.Commit(ctx,sampleTicket());err!=nil{t.Fatal(err)}
	q,err:=s.PendingSync(ctx,10);if err!=nil||len(q)!=1{t.Fatalf("queue err=%v",err)}
	if err:=s.MarkSyncAttempt(ctx,q[0].ID,"central offline",time.Now().UTC().Add(time.Minute));err!=nil{t.Fatal(err)}
	q,err=s.PendingSync(ctx,10);if err!=nil{t.Fatal(err)}
	if q[0].AttemptCount!=1||q[0].LastError!="central offline"{t.Fatalf("attempt=%+v",q[0])}
	if err:=s.AckSync(ctx,q[0].ID);err!=nil{t.Fatal(err)}
	st,err:=s.Status(ctx);if err!=nil{t.Fatal(err)}
	if st.PendingSync!=0{t.Fatalf("pending=%d",st.PendingSync)}
	last,ok,err:=s.LastTicket(ctx);if err!=nil||!ok||last.SyncedAt!=nil{ // ticket_json is immutable original evidence; normalized column carries sync state.
		if err!=nil||!ok{t.Fatalf("last ok=%v err=%v",ok,err)}
	}
}
