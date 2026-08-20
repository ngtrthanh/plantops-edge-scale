package runtimeio

import (
	"context"
	"fmt"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/ports"
)

type AuditGateError struct{ Err error }
func (e AuditGateError) Error() string { return "operational audit gate: " + e.Err.Error() }
func (e AuditGateError) Unwrap() error { return e.Err }

type AuditedOutputs struct {
	Inner         Outputs
	Audit         ports.AuditStore
	StationID     string
	RuntimeGitSHA string
	TransactionID func() string
}

func (a *AuditedOutputs) SetEntryLight(ctx context.Context, green bool) error {
	return a.command(ctx, domain.DeviceEntryBarrier, "ENTRY_SIGNAL", green, func() error { return a.Inner.SetEntryLight(ctx, green) })
}
func (a *AuditedOutputs) SetExitLight(ctx context.Context, green bool) error {
	return a.command(ctx, domain.DeviceExitBarrier, "EXIT_SIGNAL", green, func() error { return a.Inner.SetExitLight(ctx, green) })
}
func (a *AuditedOutputs) SetBuzzer(ctx context.Context, on bool) error {
	return a.command(ctx, domain.DeviceExitBarrier, "BUZZER", on, func() error { return a.Inner.SetBuzzer(ctx, on) })
}
func (a *AuditedOutputs) RequestEntryBarrier(ctx context.Context, open bool) error {
	return a.command(ctx, domain.DeviceEntryBarrier, "ENTRY_BARRIER_OPEN_REQUEST", open, func() error { return a.Inner.RequestEntryBarrier(ctx, open) })
}
func (a *AuditedOutputs) RequestExitBarrier(ctx context.Context, open bool) error {
	return a.command(ctx, domain.DeviceExitBarrier, "EXIT_BARRIER_OPEN_REQUEST", open, func() error { return a.Inner.RequestExitBarrier(ctx, open) })
}

// SafeState must never be blocked by a failed audit disk. Physical de-permissive
// action executes first; audit is best-effort afterward. The caller may expose
// audit health separately, but safety action itself is not held hostage by it.
func (a *AuditedOutputs) SafeState(ctx context.Context) error {
	if a.Inner == nil { return fmt.Errorf("inner outputs are nil") }
	if err := a.Inner.SafeState(ctx); err != nil { return err }
	_ = a.append(ctx, domain.AuditEvent{
		Kind:domain.AuditOutputResult, Source:"RUNTIME_IO", Action:"SAFE_STATE_APPLIED",
		Data:map[string]any{"entry_green":false,"exit_green":false,"buzzer":false,"entry_open_request":false,"exit_open_request":false},
	})
	return nil
}

func (a *AuditedOutputs) command(ctx context.Context, device domain.DeviceID, action string, permissive bool, execute func() error) error {
	if a.Inner == nil { return fmt.Errorf("inner outputs are nil") }
	intent := domain.AuditEvent{
		Kind:domain.AuditOutputCommand, Source:"RUNTIME_IO", Device:device,
		Action:action, Data:map[string]any{"value":permissive,"phase":"intent"},
	}
	// `true` is the permissive direction for every interface method here. It is
	// gated by durable audit before physical execution. `false` is a safe/de-
	// permissive direction and is allowed even if audit persistence is broken.
	if permissive {
		if err := a.append(ctx,intent); err != nil { return AuditGateError{Err:err} }
	}

	err := execute()
	result := domain.AuditEvent{
		Kind:domain.AuditOutputResult, Source:"RUNTIME_IO", Device:device,
		Action:action, Data:map[string]any{"value":permissive,"phase":"result","success":err==nil},
	}
	if err != nil { result.Reason=err.Error() }
	if auditErr := a.append(ctx,result); auditErr != nil {
		if permissive && err == nil {
			// The command was pre-audited, so physical truth is still reconstructable;
			// return the post-result audit failure to force lockout/reconciliation.
			return AuditGateError{Err:auditErr}
		}
		// Safe commands are never failed solely because the journal is unavailable.
	}
	return err
}

func (a *AuditedOutputs) append(ctx context.Context, event domain.AuditEvent) error {
	if a.Audit == nil { return fmt.Errorf("operational audit store is nil") }
	if event.StationID=="" { event.StationID=a.StationID }
	if event.TransactionID=="" && a.TransactionID!=nil { event.TransactionID=a.TransactionID() }
	if event.AtUTC.IsZero() { event.AtUTC=time.Now().UTC() }
	if event.Actor=="" { event.Actor="SYSTEM" }
	if event.RuntimeGitSHA=="" { event.RuntimeGitSHA=a.RuntimeGitSHA }
	_,err:=a.Audit.Append(ctx,event)
	return err
}
