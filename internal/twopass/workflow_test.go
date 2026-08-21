package twopass

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/registry"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/sqlitestore"
	cyclepkg "github.com/ngtrthanh/plantops-edge-scale/goedge/internal/cycle"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/engine"
)

func TestFullTwoPassPhysicalWorkflowQueuesCallsAndCompletesNet(t *testing.T) {
	ctx:=context.Background()
	store,err:=sqlitestore.Open(filepath.Join(t.TempDir(),"edge.db"));if err!=nil{t.Fatal(err)};defer store.Close()
	coord:=cyclepkg.New(store,domain.PairPolicy{MaxElapsed:4*time.Hour,MinNetKG:1000})
	bridge:=NewCommitBridge(coord)
	reg,err:=registry.Parse("RFID-1=15C-123.45");if err!=nil{t.Fatal(err)}
	inner:=engine.New(engine.Config{StationID:"S01",EmptyScaleMaxKG:500,MinStableWeightKG:1000,StableConfirmations:2,StableToleranceKG:20},bridge,reg)
	w:=NewWorkflow(inner,bridge,coord)

	t0:=time.Now().UTC()
	observeScale(t,w,0,true,t0,1)

	// PASS #1 A -> B.
	if err:=w.ObservePosition(ctx,pos(true,false,false,false,t0.Add(time.Second)));err!=nil{t.Fatal(err)}
	observeIdentity(t,w,"RFID-1","15C-123.45",t0.Add(2*time.Second))
	s:=w.Snapshot();if s.Transaction==nil||s.Transaction.Direction!=domain.DirectionAToB||s.Transaction.PassNumber!=domain.PassFirst{t.Fatalf("first direction snapshot=%+v",s.Transaction)}
	if !s.Transaction.Outputs.EntryBarrierOpen{t.Fatal("A->B ingress should request physical side-A barrier")}
	if err:=w.ObservePosition(ctx,pos(false,true,true,false,t0.Add(3*time.Second)));err!=nil{t.Fatal(err)}
	observeScale(t,w,28455,true,t0.Add(4*time.Second),2)
	observeScale(t,w,28460,true,t0.Add(5*time.Second),3)
	s=w.Snapshot();if s.Transaction==nil||s.Transaction.CycleStatus!=domain.CycleQueued||s.Transaction.BusinessComplete||s.Transaction.TicketID!=""{t.Fatalf("first pass must be queued, not business-complete: %+v",s.Transaction)}
	if !s.Transaction.Outputs.ExitBarrierOpen{t.Fatal("A->B first pass release should use physical side-B barrier")}
	if err:=w.ObservePosition(ctx,pos(false,false,false,true,t0.Add(6*time.Second)));err!=nil{t.Fatal(err)}
	if err:=w.ObservePosition(ctx,pos(false,false,false,false,t0.Add(7*time.Second)));err!=nil{t.Fatal(err)}
	if err:=w.ObservePosition(ctx,pos(false,false,false,false,t0.Add(8*time.Second)));err!=nil{t.Fatal(err)}
	if w.Snapshot().Transaction!=nil{t.Fatalf("physical scale must return IDLE while cycle remains queued: %+v",w.Snapshot().Transaction)}
	q,err:=coord.Queue(ctx);if err!=nil||len(q)!=1{t.Fatalf("queue=%+v err=%v",q,err)}
	if q[0].Status!=domain.CycleQueued||q[0].FirstPass.Weight.WeightKG!=28460{t.Fatalf("bad first queue cycle=%+v",q[0])}
	st,_:=store.Status(ctx);if st.Tickets!=0||st.PendingSync!=0{t.Fatalf("first pass created final ticket/sync: %+v",st)}

	// Explicitly call the correct durable cycle before return.
	if err:=coord.Call(ctx,q[0].ID);err!=nil{t.Fatal(err)}
	observeScale(t,w,0,true,t0.Add(30*time.Minute),4)

	// PASS #2 B -> A starts from physical side B (historic ExitPresent).
	if err:=w.ObservePosition(ctx,pos(false,false,false,true,t0.Add(31*time.Minute)));err!=nil{t.Fatal(err)}
	observeIdentity(t,w,"RFID-1","15C12345",t0.Add(31*time.Minute+time.Second))
	s=w.Snapshot();if s.Transaction==nil||s.Transaction.Direction!=domain.DirectionBToA||s.Transaction.PassNumber!=domain.PassSecond{t.Fatalf("second direction snapshot=%+v",s.Transaction)}
	if s.Transaction.CycleID!=q[0].ID||s.Transaction.CycleStatus!=domain.CycleCalled{t.Fatalf("return did not bind called cycle: %+v",s.Transaction)}
	if !s.Transaction.Outputs.ExitBarrierOpen{t.Fatal("B->A ingress should request physical side-B barrier")}
	if err:=w.ObservePosition(ctx,pos(false,true,true,false,t0.Add(32*time.Minute)));err!=nil{t.Fatal(err)}
	observeScale(t,w,11825,true,t0.Add(45*time.Minute),5)
	observeScale(t,w,11820,true,t0.Add(45*time.Minute+time.Second),6)
	s=w.Snapshot();if s.Transaction==nil||!s.Transaction.BusinessComplete||s.Transaction.CycleStatus!=domain.CycleComplete||s.Transaction.TicketID==""{t.Fatalf("second pass did not complete business cycle: %+v",s.Transaction)}
	if !s.Transaction.Outputs.EntryBarrierOpen{t.Fatal("B->A completed release should use physical side-A barrier")}

	st,err=store.Status(ctx);if err!=nil{t.Fatal(err)}
	if st.QueuedCycles!=0||st.CalledCycles!=0||st.CompletedCycles!=1||st.Tickets!=1||st.PendingSync!=1{t.Fatalf("durable final state=%+v",st)}
	last,ok,err:=store.LastTicket(ctx);if err!=nil||!ok{t.Fatalf("last ok=%v err=%v",ok,err)}
	if last.GrossKG!=28460||last.TareKG!=11820||last.NetKG!=16640||last.CycleID!=q[0].ID{t.Fatalf("paired final ticket=%+v",last)}
	if last.FirstWeightRawRef.Seq!=3||last.SecondWeightRawRef.Seq!=6{t.Fatalf("paired raw refs=%+v",last)}
}

func TestBToAReturnWithoutCalledCycleIsQueueMismatchAndSafe(t *testing.T){
	ctx:=context.Background()
	store,err:=sqlitestore.Open(filepath.Join(t.TempDir(),"edge.db"));if err!=nil{t.Fatal(err)};defer store.Close()
	coord:=cyclepkg.New(store,domain.PairPolicy{})
	bridge:=NewCommitBridge(coord)
	reg,_:=registry.Parse("RFID-1=15C-123.45")
	inner:=engine.New(engine.Config{StationID:"S01",EmptyScaleMaxKG:500,MinStableWeightKG:1000,StableConfirmations:2,StableToleranceKG:20},bridge,reg)
	w:=NewWorkflow(inner,bridge,coord)
	t0:=time.Now().UTC();observeScale(t,w,0,true,t0,1)
	if err:=w.ObservePosition(ctx,pos(false,false,false,true,t0.Add(time.Second)));err!=nil{t.Fatal(err)}
	observeIdentity(t,w,"RFID-1","15C-123.45",t0.Add(2*time.Second))
	s:=w.Snapshot();if s.Transaction==nil||s.Transaction.State!=domain.StateQueueMismatch||s.Transaction.CycleStatus!=domain.CycleUnpairedReturn{t.Fatalf("expected queue mismatch: %+v",s.Transaction)}
	if s.Transaction.Outputs!=(domain.DesiredOutputs{}){t.Fatalf("uncalled return must have no permissive output: %+v",s.Transaction.Outputs)}
	st,_:=store.Status(ctx);if st.Tickets!=0||st.PendingSync!=0{t.Fatalf("uncalled return created completion: %+v",st)}
}

func observeIdentity(t *testing.T,w *Workflow,rfid,plate string,at time.Time){t.Helper();ctx:=context.Background();if err:=w.ObserveRFID(ctx,domain.RFIDObservation{Tag:rfid,Health:domain.HealthHealthy,Observed:at});err!=nil{t.Fatal(err)};if err:=w.ObserveLPR(ctx,domain.LPRObservation{Plate:plate,Health:domain.HealthHealthy,Observed:at});err!=nil{t.Fatal(err)}}
func observeScale(t *testing.T,w *Workflow,kg int64,stable bool,at time.Time,seq uint64){t.Helper();if err:=w.ObserveScale(context.Background(),domain.AuditedScaleReading{Reading:domain.ScaleReading{WeightKG:kg,Stable:stable,Health:domain.HealthHealthy,Observed:at},RawRef:domain.RawWeightRef{Seq:seq,Hash:"h"}});err!=nil{t.Fatal(err)}}
func pos(entry,front,rear,exit bool,at time.Time)domain.PositionSnapshot{return domain.PositionSnapshot{EntryPresent:entry,FrontPresent:front,RearPresent:rear,ExitPresent:exit,EntryBarrierClosed:true,ExitBarrierClosed:true,SafetyClear:true,Observed:at}}
