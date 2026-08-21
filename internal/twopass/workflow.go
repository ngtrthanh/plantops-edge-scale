package twopass

import (
	"context"
	"errors"
	"sync"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/engine"
)

type InnerWorkflow interface {
	Snapshot() engine.Snapshot
	ActiveTransactionID() string
	ObservePosition(context.Context, domain.PositionSnapshot) error
	ObserveRFID(context.Context, domain.RFIDObservation) error
	ObserveLPR(context.Context, domain.LPRObservation) error
	ObserveScale(context.Context, domain.AuditedScaleReading) error
	ObserveFault(context.Context, domain.Fault) error
	ClearFault(context.Context, domain.DeviceID) error
	AuthorizeOverride(context.Context, domain.Override) error
	ResetCompleted() error
}

// Workflow normalizes fixed physical side A/B wiring into the existing logical
// one-way pass engine. Historical names are interpreted as physical sides:
//   Entry* == side A
//   Exit*  == side B
// A->B passes through unchanged. B->A swaps A/B and front/rear before entering
// the inner engine, then swaps desired outputs back to the physical sides.
type Workflow struct {
	Inner  InnerWorkflow
	Bridge *CommitBridge
	Cycles Cycles

	mu        sync.RWMutex
	direction domain.Direction
	bound     *domain.WeighCycle
}

func NewWorkflow(inner InnerWorkflow, bridge *CommitBridge, cycles Cycles) *Workflow {
	return &Workflow{Inner: inner, Bridge: bridge, Cycles: cycles}
}

func (w *Workflow) ActiveTransactionID() string {
	if w.Inner == nil { return "" }
	return w.Inner.ActiveTransactionID()
}

func (w *Workflow) ObservePosition(ctx context.Context, physical domain.PositionSnapshot) error {
	if w.Inner == nil { return errors.New("two-pass workflow inner engine is nil") }

	// Clear a completed pass only after the physical lane is fully clear. This
	// preserves a visible COMPLETE snapshot for at least one poll while allowing
	// the scale to return automatically to IDLE for the next truck.
	before := w.Inner.Snapshot()
	if before.Transaction != nil && before.Transaction.State == domain.StateComplete && allPresenceClear(physical) {
		txID := before.Transaction.ID
		if err := w.Inner.ResetCompleted(); err == nil {
			w.resetSession(txID)
		}
	}

	w.mu.Lock()
	if w.direction == "" && w.Inner.ActiveTransactionID() == "" {
		w.direction = detectDirection(physical)
		if w.Bridge != nil && w.direction != "" { w.Bridge.SetDirection(w.direction) }
	}
	direction := w.direction
	w.mu.Unlock()

	logical := physical
	if direction == domain.DirectionBToA { logical = swapPhysicalPosition(physical) }
	if err := w.Inner.ObservePosition(ctx, logical); err != nil { return err }
	w.tryBindCalled(ctx)
	return nil
}

func (w *Workflow) ObserveRFID(ctx context.Context, o domain.RFIDObservation) error {
	if err := w.Inner.ObserveRFID(ctx, o); err != nil { return err }
	w.tryBindCalled(ctx)
	return nil
}

func (w *Workflow) ObserveLPR(ctx context.Context, o domain.LPRObservation) error {
	if err := w.Inner.ObserveLPR(ctx, o); err != nil { return err }
	w.tryBindCalled(ctx)
	return nil
}

func (w *Workflow) ObserveScale(ctx context.Context, a domain.AuditedScaleReading) error {
	return w.Inner.ObserveScale(ctx, a)
}
func (w *Workflow) ObserveFault(ctx context.Context, f domain.Fault) error { return w.Inner.ObserveFault(ctx, f) }
func (w *Workflow) ClearFault(ctx context.Context, d domain.DeviceID) error { return w.Inner.ClearFault(ctx, d) }
func (w *Workflow) AuthorizeOverride(ctx context.Context, o domain.Override) error { return w.Inner.AuthorizeOverride(ctx, o) }

func (w *Workflow) ResetCompleted() error {
	if w.Inner == nil { return nil }
	s := w.Inner.Snapshot()
	txID := ""
	if s.Transaction != nil { txID = s.Transaction.ID }
	if err := w.Inner.ResetCompleted(); err != nil { return err }
	w.resetSession(txID)
	return nil
}

func (w *Workflow) Snapshot() engine.Snapshot {
	if w.Inner == nil { return engine.Snapshot{} }
	s := w.Inner.Snapshot()
	if s.Transaction == nil { return s }

	tx := cloneTx(s.Transaction)
	w.mu.RLock()
	direction := w.direction
	var bound *domain.WeighCycle
	if w.bound != nil { v := *w.bound; bound = &v }
	w.mu.RUnlock()
	if direction == "" { direction = domain.DirectionAToB }
	tx.Direction = direction
	if direction == domain.DirectionAToB { tx.PassNumber = domain.PassFirst } else { tx.PassNumber = domain.PassSecond }

	if direction == domain.DirectionBToA {
		tx.PositionSnapshot = swapPhysicalPosition(tx.PositionSnapshot)
		tx.Outputs = swapPhysicalOutputs(tx.Outputs)
	}

	if bound != nil {
		tx.CycleID = bound.ID
		tx.CycleStatus = bound.Status
	}
	if w.Bridge != nil {
		if out, ok := w.Bridge.Outcome(tx.ID); ok {
			tx.CycleID = out.Cycle.ID
			tx.CycleStatus = out.Cycle.Status
			if out.Direction == domain.DirectionAToB {
				// The inner pass engine generated a receipt ID, but first pass is
				// not a completed business ticket.
				tx.TicketID = ""
				tx.BusinessComplete = false
			} else if out.FinalTicket.ID != "" {
				tx.TicketID = out.FinalTicket.ID
				tx.BusinessComplete = true
				tx.CycleStatus = domain.CycleComplete
			}
			if out.Err != "" {
				tx.BusinessComplete = false
				tx.LastBlockReason = out.Err
			}
		}
	}

	// A returning B->A vehicle may not enter automatically until its identity
	// resolves to a durable CALLED cycle. This gate is outside the inner pass
	// engine because queue/call is business state, not a physical sensor state.
	if direction == domain.DirectionBToA && tx.Identity == domain.IdentityAccepted && bound == nil && tx.State != domain.StateFaultLockout && tx.State != domain.StateComplete {
		tx.State = domain.StateQueueMismatch
		tx.CycleStatus = domain.CycleUnpairedReturn
		tx.BusinessComplete = false
		tx.Outputs = domain.DesiredOutputs{}
		tx.LastBlockReason = "B_TO_A return has no matching CALLED cycle; automatic entry blocked"
	}

	s.Transaction = tx
	s.State = tx.State
	s.Mode = tx.Mode
	return s
}

func (w *Workflow) tryBindCalled(ctx context.Context) {
	if w.Cycles == nil || w.Inner == nil { return }
	w.mu.RLock(); direction := w.direction; w.mu.RUnlock()
	if direction != domain.DirectionBToA { return }
	s := w.Inner.Snapshot()
	if s.Transaction == nil || s.Transaction.Identity != domain.IdentityAccepted { return }
	cycle, found, err := w.Cycles.ResolveCalled(ctx, s.Transaction.LPR.Plate, s.Transaction.RFID.Tag)
	w.mu.Lock()
	defer w.mu.Unlock()
	if err != nil || !found { w.bound = nil; return }
	v := cycle
	w.bound = &v
}

func (w *Workflow) resetSession(txID string) {
	w.mu.Lock()
	w.direction = ""
	w.bound = nil
	w.mu.Unlock()
	if w.Bridge != nil { w.Bridge.Reset(txID) }
}

func detectDirection(p domain.PositionSnapshot) domain.Direction {
	switch {
	case p.EntryPresent && !p.ExitPresent:
		return domain.DirectionAToB
	case p.ExitPresent && !p.EntryPresent:
		return domain.DirectionBToA
	default:
		return ""
	}
}

func swapPhysicalPosition(p domain.PositionSnapshot) domain.PositionSnapshot {
	return domain.PositionSnapshot{
		EntryPresent: p.ExitPresent,
		FrontPresent: p.RearPresent,
		RearPresent: p.FrontPresent,
		ExitPresent: p.EntryPresent,
		EntryBarrierOpen: p.ExitBarrierOpen,
		EntryBarrierClosed: p.ExitBarrierClosed,
		ExitBarrierOpen: p.EntryBarrierOpen,
		ExitBarrierClosed: p.EntryBarrierClosed,
		SafetyClear: p.SafetyClear,
		Observed: p.Observed,
	}
}

func swapPhysicalOutputs(v domain.DesiredOutputs) domain.DesiredOutputs {
	return domain.DesiredOutputs{
		EntryGreen: v.ExitGreen,
		ExitGreen: v.EntryGreen,
		Buzzer: v.Buzzer,
		EntryBarrierOpen: v.ExitBarrierOpen,
		ExitBarrierOpen: v.EntryBarrierOpen,
	}
}

func allPresenceClear(p domain.PositionSnapshot) bool {
	return !p.EntryPresent && !p.FrontPresent && !p.RearPresent && !p.ExitPresent
}

func cloneTx(in *domain.Transaction) *domain.Transaction {
	if in == nil { return nil }
	out := *in
	out.Faults = append([]domain.Fault(nil), in.Faults...)
	out.Overrides = append([]domain.Override(nil), in.Overrides...)
	if in.LatestScale != nil { v := *in.LatestScale; out.LatestScale = &v }
	if in.AcceptedWeight != nil { v := *in.AcceptedWeight; out.AcceptedWeight = &v }
	if in.LocalCommittedAt != nil { v := *in.LocalCommittedAt; out.LocalCommittedAt = &v }
	if in.CompletedAt != nil { v := *in.CompletedAt; out.CompletedAt = &v }
	return &out
}
