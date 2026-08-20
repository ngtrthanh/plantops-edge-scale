package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
)

type fakeRegistry struct{ vehicles map[string]domain.VehicleIdentity }

func (r fakeRegistry) ResolveRFID(_ context.Context, tag string) (domain.VehicleIdentity, bool, error) {
	v, ok := r.vehicles[tag]
	return v, ok, nil
}

type fakeTicketStore struct {
	mu      sync.Mutex
	tickets []domain.Ticket
	err     error
}

func (s *fakeTicketStore) Commit(_ context.Context, t domain.Ticket) error {
	if s.err != nil {
		return s.err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tickets = append(s.tickets, t)
	return nil
}

func (s *fakeTicketStore) latest() domain.Ticket {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.tickets) == 0 {
		return domain.Ticket{}
	}
	return s.tickets[len(s.tickets)-1]
}

func newTestEngine(store *fakeTicketStore) *Engine {
	return New(Config{
		StationID: "S01", MinStableWeightKG: 1000,
		StableConfirmations: 2, StableToleranceKG: 20,
	}, store, fakeRegistry{vehicles: map[string]domain.VehicleIdentity{
		"RFID-1": {RFIDTag: "RFID-1", Plate: "15C-123.45"},
	}})
}

func beginIdentifiedTruck(t *testing.T, e *Engine) time.Time {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	if err := e.ObservePosition(ctx, domain.PositionSnapshot{EntryPresent: true, SafetyClear: true, Observed: now}); err != nil {
		t.Fatal(err)
	}
	start := e.Snapshot().Transaction.StartedAt
	at := start.Add(time.Millisecond)
	if err := e.ObserveRFID(ctx, domain.RFIDObservation{Tag: "RFID-1", Health: domain.HealthHealthy, Observed: at}); err != nil {
		t.Fatal(err)
	}
	if err := e.ObserveLPR(ctx, domain.LPRObservation{Plate: "15C12345", Health: domain.HealthHealthy, Observed: at.Add(time.Millisecond)}); err != nil {
		t.Fatal(err)
	}
	s := e.Snapshot()
	if s.State != domain.StateEntryAuthorized || s.Transaction.Identity != domain.IdentityAccepted {
		t.Fatalf("identity/entry state=%s identity=%s reason=%s", s.State, s.Transaction.Identity, s.Transaction.LastBlockReason)
	}
	if !s.Transaction.Outputs.EntryGreen || !s.Transaction.Outputs.EntryBarrierOpen {
		t.Fatalf("entry outputs not authorized: %+v", s.Transaction.Outputs)
	}
	return at
}

func audited(weight int64, stable bool, seq uint64, at time.Time) domain.AuditedScaleReading {
	return domain.AuditedScaleReading{
		Reading: domain.ScaleReading{WeightKG: weight, Stable: stable, Health: domain.HealthHealthy, Observed: at},
		RawRef: domain.RawWeightRef{Seq: seq, Hash: "hash-" + string(rune('0'+seq))},
	}
}

func TestHappyPathCommitsTicketWithExactRawWeightRef(t *testing.T) {
	ctx := context.Background()
	store := &fakeTicketStore{}
	e := newTestEngine(store)
	at := beginIdentifiedTruck(t, e)

	if err := e.ObservePosition(ctx, domain.PositionSnapshot{
		FrontPresent: true, RearPresent: true, SafetyClear: true, Observed: at.Add(2 * time.Millisecond),
	}); err != nil {
		t.Fatal(err)
	}
	if got := e.Snapshot().Transaction.Position; got != domain.PositionAccepted {
		t.Fatalf("position=%s", got)
	}

	if err := e.ObserveScale(ctx, audited(28300, false, 10, at.Add(3*time.Millisecond))); err != nil {
		t.Fatal(err)
	}
	if got := e.Snapshot().State; got != domain.StateWeighing {
		t.Fatalf("state=%s want WEIGHING", got)
	}
	if err := e.ObserveScale(ctx, audited(28455, true, 11, at.Add(4*time.Millisecond))); err != nil {
		t.Fatal(err)
	}
	if err := e.ObserveScale(ctx, audited(28460, true, 12, at.Add(5*time.Millisecond))); err != nil {
		t.Fatal(err)
	}

	s := e.Snapshot()
	if s.State != domain.StateExitAuthorized {
		t.Fatalf("state=%s want EXIT_AUTHORIZED; block=%s", s.State, s.Transaction.LastBlockReason)
	}
	if s.Transaction.AcceptedWeight == nil || s.Transaction.AcceptedWeight.RawRef.Seq != 12 {
		t.Fatalf("accepted raw ref=%+v", s.Transaction.AcceptedWeight)
	}
	if !s.Transaction.Outputs.ExitGreen || !s.Transaction.Outputs.ExitBarrierOpen {
		t.Fatalf("exit release not derived after local commit: %+v", s.Transaction.Outputs)
	}
	ticket := store.latest()
	if ticket.WeightKG != 28460 || ticket.WeightRawRef.Seq != 12 || ticket.WeightRawRef.Hash != s.Transaction.AcceptedWeight.RawRef.Hash {
		t.Fatalf("ticket is not linked to accepted raw frame: %+v", ticket)
	}

	if err := e.ObservePosition(ctx, domain.PositionSnapshot{ExitPresent: true, SafetyClear: true, Observed: at.Add(6 * time.Millisecond)}); err != nil {
		t.Fatal(err)
	}
	if err := e.ObservePosition(ctx, domain.PositionSnapshot{ExitPresent: false, SafetyClear: true, Observed: at.Add(7 * time.Millisecond)}); err != nil {
		t.Fatal(err)
	}
	if got := e.Snapshot().State; got != domain.StateComplete {
		t.Fatalf("state=%s want COMPLETE", got)
	}
}

func TestWorkflowRejectsBareUnauditedScaleReading(t *testing.T) {
	store := &fakeTicketStore{}
	e := newTestEngine(store)
	err := e.ObserveScale(context.Background(), domain.AuditedScaleReading{
		Reading: domain.ScaleReading{WeightKG: 28460, Stable: true, Health: domain.HealthHealthy, Observed: time.Now().UTC()},
	})
	if err == nil {
		t.Fatal("expected missing raw audit reference to be rejected")
	}
}

func TestIdentityMismatchBlocksEntry(t *testing.T) {
	ctx := context.Background()
	e := newTestEngine(&fakeTicketStore{})
	now := time.Now().UTC()
	_ = e.ObservePosition(ctx, domain.PositionSnapshot{EntryPresent: true, SafetyClear: true, Observed: now})
	at := e.Snapshot().Transaction.StartedAt.Add(time.Millisecond)
	_ = e.ObserveRFID(ctx, domain.RFIDObservation{Tag: "RFID-1", Health: domain.HealthHealthy, Observed: at})
	_ = e.ObserveLPR(ctx, domain.LPRObservation{Plate: "15C-999.99", Health: domain.HealthHealthy, Observed: at.Add(time.Millisecond)})
	s := e.Snapshot()
	if s.State != domain.StateIdentityMismatch || s.Transaction.Identity != domain.IdentityMismatch {
		t.Fatalf("state=%s identity=%s", s.State, s.Transaction.Identity)
	}
	if s.Transaction.Outputs.EntryBarrierOpen || s.Transaction.Outputs.EntryGreen {
		t.Fatal("identity mismatch must not authorize entry")
	}
}

func TestSinglePositionSensorFaultRequiresTransactionOverride(t *testing.T) {
	ctx := context.Background()
	e := newTestEngine(&fakeTicketStore{})
	at := beginIdentifiedTruck(t, e)

	if err := e.ObserveFault(ctx, domain.Fault{
		Device: domain.DeviceFrontSensor, Health: domain.HealthFault,
		Reason: "no signal", Overridable: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.ObservePosition(ctx, domain.PositionSnapshot{RearPresent: true, SafetyClear: true, Observed: at.Add(2 * time.Millisecond)}); err != nil {
		t.Fatal(err)
	}
	if got := e.Snapshot().Transaction.Position; got == domain.PositionAccepted {
		t.Fatal("position must not be accepted before override evidence")
	}

	txID := e.ActiveTransactionID()
	if err := e.AuthorizeOverride(ctx, domain.Override{
		TransactionID: txID, Device: domain.DeviceFrontSensor, Reason: "front sensor no signal",
		RequestedBy: "operator01", AuthorizedBy: "operator01", AuthorizedAs: domain.RoleOperator,
		Evidence: []string{domain.EvidencePositionConfirmed},
	}); err != nil {
		t.Fatal(err)
	}
	s := e.Snapshot()
	if s.Transaction.Position != domain.PositionAccepted || s.Mode != domain.ModeDegraded {
		t.Fatalf("position=%s mode=%s block=%s", s.Transaction.Position, s.Mode, s.Transaction.LastBlockReason)
	}
}

func TestBothDeckSensorFaultsForceManual(t *testing.T) {
	ctx := context.Background()
	e := newTestEngine(&fakeTicketStore{})
	_ = beginIdentifiedTruck(t, e)
	_ = e.ObserveFault(ctx, domain.Fault{Device: domain.DeviceFrontSensor, Health: domain.HealthFault, Reason: "front failed", Overridable: true})
	_ = e.ObserveFault(ctx, domain.Fault{Device: domain.DeviceRearSensor, Health: domain.HealthFault, Reason: "rear failed", Overridable: true})
	s := e.Snapshot()
	if s.Mode != domain.ModeManual || s.Transaction.Position != domain.PositionManual {
		t.Fatalf("mode=%s position=%s", s.Mode, s.Transaction.Position)
	}
}

func TestScaleFaultLocksOutAndDropsAllDesiredOutputs(t *testing.T) {
	ctx := context.Background()
	e := newTestEngine(&fakeTicketStore{})
	_ = beginIdentifiedTruck(t, e)
	if err := e.ObserveFault(ctx, domain.Fault{
		Device: domain.DeviceScale, Health: domain.HealthDisconnected,
		Reason: "TCP disconnected", Overridable: false, Critical: true,
	}); err != nil {
		t.Fatal(err)
	}
	s := e.Snapshot()
	if s.State != domain.StateFaultLockout || s.Mode != domain.ModeLockout {
		t.Fatalf("state=%s mode=%s", s.State, s.Mode)
	}
	if s.Transaction.Outputs != (domain.DesiredOutputs{}) {
		t.Fatalf("lockout must remove software motion requests: %+v", s.Transaction.Outputs)
	}
}

func TestTicketCommitFailurePreventsRelease(t *testing.T) {
	ctx := context.Background()
	store := &fakeTicketStore{err: errors.New("disk full")}
	e := newTestEngine(store)
	at := beginIdentifiedTruck(t, e)
	_ = e.ObservePosition(ctx, domain.PositionSnapshot{FrontPresent: true, RearPresent: true, SafetyClear: true, Observed: at.Add(time.Millisecond)})
	_ = e.ObserveScale(ctx, audited(28460, true, 21, at.Add(2*time.Millisecond)))
	err := e.ObserveScale(ctx, audited(28460, true, 22, at.Add(3*time.Millisecond)))
	if err == nil {
		t.Fatal("expected durable ticket commit failure")
	}
	s := e.Snapshot()
	if s.State != domain.StateFaultLockout || s.Transaction.LocalCommittedAt != nil {
		t.Fatalf("state=%s committed=%v", s.State, s.Transaction.LocalCommittedAt)
	}
	if s.Transaction.Outputs.ExitBarrierOpen || s.Transaction.Outputs.ExitGreen {
		t.Fatal("failed local commit must never authorize release")
	}
}
