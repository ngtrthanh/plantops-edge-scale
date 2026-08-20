package auditjournal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
)

func TestAppendTailVerifyAndResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	j := &Journal{Path: path}
	ctx := context.Background()
	for i, kind := range []domain.AuditKind{domain.AuditTransactionStarted, domain.AuditStateTransition, domain.AuditTicketCommitted} {
		ref, err := j.Append(ctx, domain.AuditEvent{
			StationID: "S01", TransactionID: "TX-1", AtUTC: time.Now().UTC(),
			Kind: kind, Source: "TEST", Data: map[string]any{"n": i},
		})
		if err != nil { t.Fatal(err) }
		if ref.Seq != uint64(i+1) || ref.Hash == "" { t.Fatalf("bad ref: %+v", ref) }
	}
	if err := j.Verify(); err != nil { t.Fatal(err) }
	rows, err := j.Tail(2)
	if err != nil { t.Fatal(err) }
	if len(rows) != 2 || rows[0].Seq != 2 || rows[1].Seq != 3 { t.Fatalf("tail=%+v", rows) }

	j2 := &Journal{Path: path}
	ref, err := j2.Append(ctx, domain.AuditEvent{StationID:"S01", AtUTC:time.Now().UTC(), Kind:domain.AuditTransactionDone, Source:"TEST"})
	if err != nil { t.Fatal(err) }
	if ref.Seq != 4 { t.Fatalf("resume seq=%d want 4", ref.Seq) }
	if err := j2.Verify(); err != nil { t.Fatal(err) }
}

func TestComplexTypedDataRoundTripsWithoutChangingHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	j := &Journal{Path:path}
	now := time.Now().UTC()
	event := domain.AuditEvent{
		StationID:"S01", TransactionID:"TX-1", AtUTC:now,
		Kind:domain.AuditDesiredOutputs, Source:"WORKFLOW", Action:"desired outputs changed",
		Data:map[string]any{
			"raw_seq": uint64(123),
			"weight_kg": int64(28460),
			"observed_at": now,
			"position": domain.PositionSnapshot{FrontPresent:true, RearPresent:true, SafetyClear:true, Observed:now},
			"after": domain.DesiredOutputs{ExitGreen:true, ExitBarrierOpen:true, Buzzer:true},
		},
	}
	if _,err:=j.Append(context.Background(),event);err!=nil{t.Fatal(err)}
	if err:=j.Verify();err!=nil{t.Fatal(err)}
	rows,err:=j.Tail(1);if err!=nil{t.Fatal(err)}
	if len(rows)!=1 || rows[0].Event.Kind!=domain.AuditDesiredOutputs{t.Fatalf("tail=%+v",rows)}
}

func TestVerifySerializedWithConcurrentAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	j := &Journal{Path:path}
	ctx := context.Background()
	if _,err:=j.Append(ctx,domain.AuditEvent{StationID:"S01",AtUTC:time.Now().UTC(),Kind:domain.AuditTransactionStarted,Source:"TEST"});err!=nil{t.Fatal(err)}

	var wg sync.WaitGroup
	for g:=0;g<4;g++{
		g:=g
		wg.Add(1)
		go func(){
			defer wg.Done()
			for i:=0;i<20;i++{
				_,err:=j.Append(ctx,domain.AuditEvent{StationID:"S01",AtUTC:time.Now().UTC(),Kind:domain.AuditStateTransition,Source:"TEST",Data:map[string]any{"writer":g,"i":i}})
				if err!=nil{t.Errorf("append: %v",err);return}
			}
		}()
	}
	for i:=0;i<20;i++{
		if err:=j.Verify();err!=nil{t.Fatalf("verify during append %d: %v",i,err)}
	}
	wg.Wait()
	if err:=j.Verify();err!=nil{t.Fatal(err)}
	rows,err:=j.Tail(1000);if err!=nil{t.Fatal(err)}
	if got,want:=len(rows),81;got!=want{t.Fatalf("records=%d want %d",got,want)}
	for i,r:=range rows{if r.Seq!=uint64(i+1){t.Fatalf("seq[%d]=%d",i,r.Seq)}}
}

func TestVerifyDetectsTamper(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	j := &Journal{Path:path}
	_, _ = j.Append(context.Background(), domain.AuditEvent{StationID:"S01", AtUTC:time.Now().UTC(), Kind:domain.AuditStateTransition, Source:"TEST", Reason:"original"})
	b, err := os.ReadFile(path)
	if err != nil { t.Fatal(err) }
	needle := []byte("original")
	replacement := []byte("tampered")
	for i := 0; i+len(needle) <= len(b); i++ {
		if string(b[i:i+len(needle)]) == string(needle) { copy(b[i:i+len(replacement)], replacement); break }
	}
	if err := os.WriteFile(path,b,0o644); err != nil { t.Fatal(err) }
	if err := Verify(path); err == nil { t.Fatal("expected tamper detection") } else if fmt.Sprint(err)=="" { t.Fatal("empty verify error") }
}
