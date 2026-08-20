package workflowaudit

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/engine"
)

type fakeWorkflow struct { mu sync.Mutex; snap engine.Snapshot; faults []domain.Fault }
func (f *fakeWorkflow) Snapshot() engine.Snapshot { f.mu.Lock(); defer f.mu.Unlock(); return f.snap }
func (f *fakeWorkflow) ObserveFault(_ context.Context,x domain.Fault)error{f.mu.Lock();defer f.mu.Unlock();f.faults=append(f.faults,x);return nil}

type auditMem struct { mu sync.Mutex; events []domain.AuditEvent; err error }
func (a *auditMem) Append(_ context.Context,e domain.AuditEvent)(domain.AuditRef,error){a.mu.Lock();defer a.mu.Unlock();if a.err!=nil{return domain.AuditRef{},a.err};a.events=append(a.events,e);return domain.AuditRef{Seq:uint64(len(a.events)),Hash:"h"},nil}

func TestWatcherRecordsMeaningfulChangesNotEveryPoll(t *testing.T){
	wf:=&fakeWorkflow{snap:engine.Snapshot{StationID:"S01",State:domain.StateIdle,Mode:domain.ModeNormal}}
	a:=&auditMem{}
	w:=&Watcher{Workflow:wf,Audit:a,StationID:"S01",RuntimeGitSHA:"sha"}
	ctx:=context.Background()
	if err:=w.step(ctx);err!=nil{t.Fatal(err)}
	if err:=w.step(ctx);err!=nil{t.Fatal(err)}
	if len(a.events)!=0{t.Fatalf("unchanged IDLE produced events: %d",len(a.events))}

	now:=time.Now().UTC()
	wf.mu.Lock(); wf.snap=engine.Snapshot{StationID:"S01",State:domain.StateIdentifying,Mode:domain.ModeNormal,Transaction:&domain.Transaction{ID:"TX-1",StationID:"S01",State:domain.StateIdentifying,Mode:domain.ModeNormal,StartedAt:now,UpdatedAt:now,Identity:domain.IdentityPending,Position:domain.PositionPending}}; wf.mu.Unlock()
	if err:=w.step(ctx);err!=nil{t.Fatal(err)}
	if len(a.events)!=1 || a.events[0].Kind!=domain.AuditTransactionStarted{t.Fatalf("events=%+v",a.events)}

	wf.mu.Lock(); tx:=*wf.snap.Transaction; tx.State=domain.StateEntryAuthorized; tx.Identity=domain.IdentityAccepted; tx.IdentityReason="RFID/LPR match"; tx.RFID=domain.RFIDObservation{Tag:"RFID-1",Health:domain.HealthHealthy,Observed:now}; tx.LPR=domain.LPRObservation{Plate:"15C-123.45",Health:domain.HealthHealthy,Observed:now}; tx.Outputs=domain.DesiredOutputs{EntryGreen:true,EntryBarrierOpen:true}; wf.snap.State=tx.State; wf.snap.Transaction=&tx; wf.mu.Unlock()
	if err:=w.step(ctx);err!=nil{t.Fatal(err)}

	seen:=map[domain.AuditKind]bool{}
	for _,e:=range a.events{seen[e.Kind]=true;if e.RuntimeGitSHA!="sha"{t.Fatalf("missing runtime sha: %+v",e)}}
	for _,kind:=range []domain.AuditKind{domain.AuditStateTransition,domain.AuditRFIDObserved,domain.AuditLPRObserved,domain.AuditIdentityDecision,domain.AuditDesiredOutputs}{if !seen[kind]{t.Fatalf("missing audit kind %s; events=%+v",kind,a.events)}}

	before:=len(a.events)
	if err:=w.step(ctx);err!=nil{t.Fatal(err)}
	if len(a.events)!=before{t.Fatalf("unchanged snapshot generated duplicate events: %d -> %d",before,len(a.events))}
}
