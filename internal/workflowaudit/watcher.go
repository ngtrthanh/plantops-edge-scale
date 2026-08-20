package workflowaudit

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/engine"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/ports"
)

type Workflow interface {
	Snapshot() engine.Snapshot
	ObserveFault(context.Context, domain.Fault) error
}

type Watcher struct {
	Workflow      Workflow
	Audit         ports.AuditStore
	StationID     string
	RuntimeGitSHA string
	Interval      time.Duration

	last *engine.Snapshot
}

func (w *Watcher) Run(ctx context.Context) error {
	if w.Workflow == nil || w.Audit == nil { return fmt.Errorf("workflow audit watcher requires workflow and audit store") }
	ticker := time.NewTicker(w.interval())
	defer ticker.Stop()
	for {
		if err := w.step(ctx); err != nil {
			_ = w.Workflow.ObserveFault(ctx, domain.Fault{
				Device: domain.DeviceAuditStore, Health: domain.HealthFault,
				Reason: "operational audit append failed: " + err.Error(), Overridable: false, Critical: true,
			})
		}
		select {
		case <-ctx.Done(): return nil
		case <-ticker.C:
		}
	}
}

func (w *Watcher) step(ctx context.Context) error {
	cur := w.Workflow.Snapshot()
	if w.last == nil {
		copy := cur
		w.last = &copy
		if cur.Transaction != nil {
			return w.append(ctx, domain.AuditEvent{
				TransactionID: cur.Transaction.ID, Kind: domain.AuditTransactionStarted,
				Source: "WORKFLOW", Action: "transaction observed", Data: map[string]any{"state": cur.State, "mode": cur.Mode},
			})
		}
		return nil
	}
	prev := *w.last
	events := diff(prev, cur)
	for _, event := range events {
		if err := w.append(ctx, event); err != nil { return err }
	}
	copy := cur
	w.last = &copy
	return nil
}

func (w *Watcher) append(ctx context.Context, event domain.AuditEvent) error {
	if event.StationID == "" { event.StationID = w.StationID }
	if event.AtUTC.IsZero() { event.AtUTC = time.Now().UTC() }
	if event.Actor == "" { event.Actor = "SYSTEM" }
	if event.RuntimeGitSHA == "" { event.RuntimeGitSHA = w.RuntimeGitSHA }
	_, err := w.Audit.Append(ctx, event)
	return err
}

func diff(prev, cur engine.Snapshot) []domain.AuditEvent {
	out := make([]domain.AuditEvent, 0, 8)
	pt, ct := prev.Transaction, cur.Transaction
	if pt == nil && ct != nil {
		out = append(out, domain.AuditEvent{TransactionID:ct.ID, Kind:domain.AuditTransactionStarted, Source:"WORKFLOW", Action:"transaction started", NewState:ct.State})
	}
	if ct == nil { return out }
	txID := ct.ID
	if pt != nil && pt.ID == txID {
		if pt.State != ct.State {
			kind := domain.AuditStateTransition
			if ct.State == domain.StateComplete { kind = domain.AuditTransactionDone }
			out = append(out, domain.AuditEvent{TransactionID:txID, Kind:kind, Source:"WORKFLOW", Action:"state transition", OldState:pt.State, NewState:ct.State, Reason:ct.LastBlockReason})
		}
		if !reflect.DeepEqual(pt.RFID, ct.RFID) && ct.RFID.Tag != "" {
			out = append(out, domain.AuditEvent{TransactionID:txID, Kind:domain.AuditRFIDObserved, Source:"RFID", Device:domain.DeviceRFID, Action:"observation", Data:map[string]any{"tag":ct.RFID.Tag,"quality":ct.RFID.Quality,"health":ct.RFID.Health,"observed_at":ct.RFID.Observed}})
		}
		if !reflect.DeepEqual(pt.LPR, ct.LPR) && ct.LPR.Plate != "" {
			out = append(out, domain.AuditEvent{TransactionID:txID, Kind:domain.AuditLPRObserved, Source:"LPR", Device:domain.DeviceLPR, Action:"observation", Data:map[string]any{"plate":ct.LPR.Plate,"confidence":ct.LPR.Confidence,"image_ref":ct.LPR.ImageRef,"health":ct.LPR.Health,"observed_at":ct.LPR.Observed}})
		}
		if pt.Identity != ct.Identity || pt.IdentityReason != ct.IdentityReason {
			out = append(out, domain.AuditEvent{TransactionID:txID, Kind:domain.AuditIdentityDecision, Source:"WORKFLOW", Action:string(ct.Identity), Reason:ct.IdentityReason, Data:map[string]any{"rfid":ct.RFID.Tag,"plate":ct.LPR.Plate}})
		}
		if pt.Position != ct.Position {
			out = append(out, domain.AuditEvent{TransactionID:txID, Kind:domain.AuditPositionDecision, Source:"WORKFLOW", Action:string(ct.Position), Data:map[string]any{"position":ct.PositionSnapshot}})
		}
		for _, f := range addedFaults(pt.Faults, ct.Faults) {
			out = append(out, domain.AuditEvent{TransactionID:txID, Kind:domain.AuditFaultSet, Source:"WORKFLOW", Device:f.Device, Action:"fault set", Reason:f.Reason, Data:map[string]any{"health":f.Health,"critical":f.Critical,"overridable":f.Overridable}})
		}
		for _, f := range removedFaults(pt.Faults, ct.Faults) {
			out = append(out, domain.AuditEvent{TransactionID:txID, Kind:domain.AuditFaultCleared, Source:"WORKFLOW", Device:f.Device, Action:"fault cleared", Reason:f.Reason})
		}
		if len(ct.Overrides) > len(pt.Overrides) {
			for _, o := range ct.Overrides[len(pt.Overrides):] {
				out = append(out, domain.AuditEvent{TransactionID:txID, Kind:domain.AuditOverrideAuthorized, Source:"WORKFLOW", Device:o.Device, Actor:o.AuthorizedBy, Action:"override authorized", Reason:o.Reason, Evidence:append([]string(nil),o.Evidence...), Data:map[string]any{"requested_by":o.RequestedBy,"authorized_as":o.AuthorizedAs,"authorized_at":o.AuthorizedAt}})
			}
		}
		if pt.AcceptedWeight == nil && ct.AcceptedWeight != nil {
			out = append(out, domain.AuditEvent{TransactionID:txID, Kind:domain.AuditStableAccepted, Source:"SCALE", Device:domain.DeviceScale, Action:"stable weight accepted", Data:map[string]any{"weight_kg":ct.AcceptedWeight.WeightKG,"observed_at":ct.AcceptedWeight.ObservedAt,"raw_seq":ct.AcceptedWeight.RawRef.Seq,"raw_hash":ct.AcceptedWeight.RawRef.Hash}})
		}
		if pt.TicketID == "" && ct.TicketID != "" {
			out = append(out, domain.AuditEvent{TransactionID:txID, Kind:domain.AuditTicketCommitted, Source:"WORKFLOW", Action:"local durable ticket committed", Data:map[string]any{"ticket_id":ct.TicketID,"committed_at":ct.LocalCommittedAt}})
		}
		if !reflect.DeepEqual(pt.Outputs, ct.Outputs) {
			out = append(out, domain.AuditEvent{TransactionID:txID, Kind:domain.AuditDesiredOutputs, Source:"WORKFLOW", Action:"desired outputs changed", Data:map[string]any{"before":pt.Outputs,"after":ct.Outputs}})
		}
	}
	return out
}

func addedFaults(old, cur []domain.Fault) []domain.Fault { return faultDiff(old,cur) }
func removedFaults(old, cur []domain.Fault) []domain.Fault { return faultDiff(cur,old) }
func faultDiff(base, compare []domain.Fault) []domain.Fault {
	seen := map[string]bool{}
	for _, f := range base { seen[faultKey(f)] = true }
	out := []domain.Fault{}
	for _, f := range compare { if !seen[faultKey(f)] { out=append(out,f) } }
	sort.Slice(out,func(i,j int)bool{return string(out[i].Device)<string(out[j].Device)})
	return out
}
func faultKey(f domain.Fault) string { return strings.Join([]string{string(f.Device),string(f.Health),f.Reason,fmt.Sprint(f.Critical),fmt.Sprint(f.Overridable)},"|") }

func (w *Watcher) interval() time.Duration { if w.Interval<=0 { return 50*time.Millisecond }; return w.Interval }
