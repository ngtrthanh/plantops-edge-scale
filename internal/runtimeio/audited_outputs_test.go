package runtimeio

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
)

type memoryAudit struct {
	mu sync.Mutex
	events []domain.AuditEvent
	err error
}
func (m *memoryAudit) Append(_ context.Context,e domain.AuditEvent)(domain.AuditRef,error){
	m.mu.Lock(); defer m.mu.Unlock()
	if m.err!=nil{return domain.AuditRef{},m.err}
	m.events=append(m.events,e)
	return domain.AuditRef{Seq:uint64(len(m.events)),Hash:"hash"},nil
}

type countingOutputs struct { entryOpenCalls int; entryOpen bool; entryGreenCalls int; entryGreen bool; safeCalls int }
func (o *countingOutputs) SetEntryLight(_ context.Context,v bool)error{o.entryGreenCalls++;o.entryGreen=v;return nil}
func (o *countingOutputs) SetExitLight(context.Context,bool)error{return nil}
func (o *countingOutputs) SetBuzzer(context.Context,bool)error{return nil}
func (o *countingOutputs) RequestEntryBarrier(_ context.Context,v bool)error{o.entryOpenCalls++;o.entryOpen=v;return nil}
func (o *countingOutputs) RequestExitBarrier(context.Context,bool)error{return nil}
func (o *countingOutputs) SafeState(context.Context)error{o.safeCalls++;o.entryOpen=false;o.entryGreen=false;return nil}

func TestPermissiveBarrierCommandBlockedWhenAuditAppendFails(t *testing.T){
	inner:=&countingOutputs{}
	audit:=&memoryAudit{err:errors.New("disk full")}
	failed:=false
	out:=&AuditedOutputs{Inner:inner,Audit:audit,StationID:"S01",OnAuditFailure:func(error){failed=true}}
	err:=out.RequestEntryBarrier(context.Background(),true)
	if err==nil {t.Fatal("expected audit gate failure")}
	var gate AuditGateError
	if !errors.As(err,&gate){t.Fatalf("error=%T %v, want AuditGateError",err,err)}
	if inner.entryOpenCalls!=0 {t.Fatalf("physical OPEN executed despite failed audit: calls=%d",inner.entryOpenCalls)}
	if !failed {t.Fatal("audit failure callback not invoked")}
}

func TestSafeBarrierDropExecutesEvenWhenAuditIsDown(t *testing.T){
	inner:=&countingOutputs{entryOpen:true}
	audit:=&memoryAudit{err:errors.New("disk full")}
	out:=&AuditedOutputs{Inner:inner,Audit:audit,StationID:"S01"}
	if err:=out.RequestEntryBarrier(context.Background(),false);err!=nil{t.Fatalf("safe OFF must not be blocked by audit failure: %v",err)}
	if inner.entryOpenCalls!=1 || inner.entryOpen {t.Fatalf("safe OFF did not execute: calls=%d open=%v",inner.entryOpenCalls,inner.entryOpen)}
}

func TestPermissiveCommandHasIntentBeforeResult(t *testing.T){
	inner:=&countingOutputs{}
	audit:=&memoryAudit{}
	out:=&AuditedOutputs{Inner:inner,Audit:audit,StationID:"S01",RuntimeGitSHA:"abc",TransactionID:func()string{return "TX-1"}}
	if err:=out.SetEntryLight(context.Background(),true);err!=nil{t.Fatal(err)}
	if len(audit.events)!=2{t.Fatalf("events=%d want 2",len(audit.events))}
	if audit.events[0].Kind!=domain.AuditOutputCommand || audit.events[1].Kind!=domain.AuditOutputResult{t.Fatalf("unexpected audit order: %+v",audit.events)}
	if audit.events[0].TransactionID!="TX-1" || audit.events[0].RuntimeGitSHA!="abc"{t.Fatalf("audit identity missing: %+v",audit.events[0])}
	if inner.entryGreenCalls!=1 || !inner.entryGreen{t.Fatal("physical GREEN not executed after durable audit intent")}
}

func TestSafeStateExecutesWhenAuditIsDown(t *testing.T){
	inner:=&countingOutputs{entryOpen:true,entryGreen:true}
	out:=&AuditedOutputs{Inner:inner,Audit:&memoryAudit{err:errors.New("disk full")},StationID:"S01"}
	if err:=out.SafeState(context.Background());err!=nil{t.Fatal(err)}
	if inner.safeCalls!=1 || inner.entryOpen || inner.entryGreen{t.Fatalf("safe state not applied: %+v",inner)}
}
