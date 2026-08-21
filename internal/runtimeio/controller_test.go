package runtimeio

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/engine"
)

type fakeWorkflow struct {
	mu sync.Mutex
	snap engine.Snapshot
	faults map[domain.DeviceID]domain.Fault
	lastPosition domain.PositionSnapshot
}
func (w *fakeWorkflow) Snapshot() engine.Snapshot { w.mu.Lock(); defer w.mu.Unlock(); return w.snap }
func (w *fakeWorkflow) ObservePosition(_ context.Context, p domain.PositionSnapshot) error { w.mu.Lock(); defer w.mu.Unlock(); w.lastPosition = p; return nil }
func (w *fakeWorkflow) ObserveFault(_ context.Context, f domain.Fault) error { w.mu.Lock(); defer w.mu.Unlock(); if w.faults==nil { w.faults=map[domain.DeviceID]domain.Fault{} }; w.faults[f.Device]=f; return nil }
func (w *fakeWorkflow) ClearFault(_ context.Context, d domain.DeviceID) error { w.mu.Lock(); defer w.mu.Unlock(); delete(w.faults,d); return nil }
func (w *fakeWorkflow) fault(d domain.DeviceID) (domain.Fault,bool) { w.mu.Lock(); defer w.mu.Unlock(); f,ok:=w.faults[d]; return f,ok }

type fakeInputs struct { p domain.PositionSnapshot; err error }
func (i *fakeInputs) ReadInputs(context.Context) (domain.PositionSnapshot,error) { if i.err!=nil { return domain.PositionSnapshot{},i.err }; p:=i.p; if p.Observed.IsZero(){p.Observed=time.Now().UTC()}; return p,nil }

type fakeOutputs struct {
	safeCalls int
	entryGreen, exitGreen, buzzer, entryOpen, exitOpen bool
}
func (o *fakeOutputs) SetEntryLight(_ context.Context,v bool) error { o.entryGreen=v; return nil }
func (o *fakeOutputs) SetExitLight(_ context.Context,v bool) error { o.exitGreen=v; return nil }
func (o *fakeOutputs) SetBuzzer(_ context.Context,v bool) error { o.buzzer=v; return nil }
func (o *fakeOutputs) RequestEntryBarrier(_ context.Context,v bool) error { o.entryOpen=v; return nil }
func (o *fakeOutputs) RequestExitBarrier(_ context.Context,v bool) error { o.exitOpen=v; return nil }
func (o *fakeOutputs) SafeState(context.Context) error { o.safeCalls++; o.entryGreen=false; o.exitGreen=false; o.buzzer=false; o.entryOpen=false; o.exitOpen=false; return nil }

func TestGreenIsGatedByBarrierOpenFeedback(t *testing.T){
	wf:=&fakeWorkflow{snap:engine.Snapshot{Transaction:&domain.Transaction{Outputs:domain.DesiredOutputs{EntryGreen:true,EntryBarrierOpen:true}}}}
	in:=&fakeInputs{p:domain.PositionSnapshot{EntryBarrierClosed:true,SafetyClear:true}}
	out:=&fakeOutputs{}
	c:=&Controller{Workflow:wf,Inputs:in,Outputs:out,Monitor:NewMonitor(true)}
	if err:=c.step(context.Background()); err!=nil { t.Fatal(err) }
	if out.safeCalls!=1 || !out.entryOpen { t.Fatalf("safe=%d entryOpen=%v",out.safeCalls,out.entryOpen) }
	if out.entryGreen { t.Fatal("GREEN must stay off until barrier OPEN feedback") }

	in.p.EntryBarrierClosed=false; in.p.EntryBarrierOpen=true
	if err:=c.step(context.Background()); err!=nil { t.Fatal(err) }
	if !out.entryGreen { t.Fatal("GREEN should turn on after OPEN feedback") }
}

func TestRemoteIOFailureBecomesCriticalFaultAndRequiresSafeReinit(t *testing.T){
	wf:=&fakeWorkflow{}
	in:=&fakeInputs{err:errors.New("connection refused")}
	out:=&fakeOutputs{}
	c:=&Controller{Workflow:wf,Inputs:in,Outputs:out,Monitor:NewMonitor(true),safeApplied:true}
	if err:=c.step(context.Background()); err==nil { t.Fatal("expected I/O error") }
	f,ok:=wf.fault(domain.DeviceRemoteIO)
	if !ok || !f.Critical || f.Overridable { t.Fatalf("unexpected remote I/O fault: %+v",f) }
	if c.safeApplied { t.Fatal("reconnect must require SafeState before commands") }

	in.err=nil; in.p=domain.PositionSnapshot{EntryBarrierClosed:true,ExitBarrierClosed:true,SafetyClear:true}
	if err:=c.step(context.Background()); err!=nil { t.Fatal(err) }
	if out.safeCalls!=1 { t.Fatalf("SafeState calls=%d want 1",out.safeCalls) }
}

func TestBuzzerNeedsRequestedBarrierOpenFeedback(t *testing.T){
	wf:=&fakeWorkflow{snap:engine.Snapshot{Transaction:&domain.Transaction{Outputs:domain.DesiredOutputs{Buzzer:true,ExitBarrierOpen:true}}}}
	in:=&fakeInputs{p:domain.PositionSnapshot{EntryBarrierClosed:true,ExitBarrierClosed:true,SafetyClear:true}}
	out:=&fakeOutputs{}
	c:=&Controller{Workflow:wf,Inputs:in,Outputs:out,Monitor:NewMonitor(true),BuzzerPulse:time.Millisecond}
	if err:=c.step(context.Background()); err!=nil { t.Fatal(err) }
	if out.buzzer { t.Fatal("buzzer must not start before requested exit barrier confirms OPEN") }
	in.p.ExitBarrierClosed=false;in.p.ExitBarrierOpen=true
	if err:=c.step(context.Background()); err!=nil { t.Fatal(err) }
	if !out.buzzer { t.Fatal("buzzer pulse did not start after exit OPEN feedback") }
	time.Sleep(3*time.Millisecond)
	if err:=c.step(context.Background()); err!=nil { t.Fatal(err) }
	if out.buzzer { t.Fatal("buzzer must switch off after bounded pulse") }
	if err:=c.step(context.Background()); err!=nil { t.Fatal(err) }
	if out.buzzer { t.Fatal("persistent desired=true must not retrigger without a false edge") }
}

func TestBuzzerAlsoGatesOnPhysicalSideAForBToA(t *testing.T){
	// B->A wrapper maps logical egress to physical side A, represented by the
	// historic EntryBarrierOpen field. The buzzer must wait for side-A feedback.
	wf:=&fakeWorkflow{snap:engine.Snapshot{Transaction:&domain.Transaction{Outputs:domain.DesiredOutputs{Buzzer:true,EntryBarrierOpen:true}}}}
	in:=&fakeInputs{p:domain.PositionSnapshot{EntryBarrierClosed:true,ExitBarrierClosed:true,SafetyClear:true}}
	out:=&fakeOutputs{}
	c:=&Controller{Workflow:wf,Inputs:in,Outputs:out,Monitor:NewMonitor(true),BuzzerPulse:time.Second}
	if err:=c.step(context.Background());err!=nil{t.Fatal(err)}
	if out.buzzer{t.Fatal("B->A buzzer must wait for physical side-A OPEN feedback")}
	in.p.EntryBarrierClosed=false;in.p.EntryBarrierOpen=true
	if err:=c.step(context.Background());err!=nil{t.Fatal(err)}
	if !out.buzzer{t.Fatal("B->A buzzer should pulse after physical side-A OPEN feedback")}
}

func TestBuzzerWithoutBarrierRequestIsNeverPermissive(t *testing.T){
	wf:=&fakeWorkflow{snap:engine.Snapshot{Transaction:&domain.Transaction{Outputs:domain.DesiredOutputs{Buzzer:true}}}}
	in:=&fakeInputs{p:domain.PositionSnapshot{EntryBarrierClosed:true,ExitBarrierClosed:true,SafetyClear:true}}
	out:=&fakeOutputs{}
	c:=&Controller{Workflow:wf,Inputs:in,Outputs:out,Monitor:NewMonitor(true)}
	if err:=c.step(context.Background());err!=nil{t.Fatal(err)}
	if out.buzzer{t.Fatal("buzzer request without a barrier release request must stay off")}
}

func TestBarrierOpenFeedbackTimeoutFaultsBarrier(t *testing.T){
	wf:=&fakeWorkflow{snap:engine.Snapshot{Transaction:&domain.Transaction{Outputs:domain.DesiredOutputs{ExitGreen:true,ExitBarrierOpen:true}}}}
	in:=&fakeInputs{p:domain.PositionSnapshot{ExitBarrierClosed:true,SafetyClear:true}}
	out:=&fakeOutputs{}
	c:=&Controller{Workflow:wf,Inputs:in,Outputs:out,Monitor:NewMonitor(true),BarrierFeedbackTimeout:time.Millisecond}
	if err:=c.step(context.Background()); err!=nil { t.Fatal(err) }
	time.Sleep(3*time.Millisecond)
	if err:=c.step(context.Background()); err!=nil { t.Fatal(err) }
	f,ok:=wf.fault(domain.DeviceExitBarrier)
	if !ok || !f.Critical { t.Fatalf("expected critical exit barrier fault, got %+v",f) }
	if out.exitGreen { t.Fatal("exit GREEN must remain off without OPEN feedback") }
}
