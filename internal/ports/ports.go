package ports

import (
	"context"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
)

type Scale interface {
	Read(context.Context) (domain.ScaleReading, error)
}

type RawWeightJournal interface {
	Append(context.Context, domain.RawWeightEvent) error
}

type AuditStore interface {
	Append(context.Context, domain.AuditEvent) (domain.AuditRef, error)
}

type PositionIO interface {
	ReadInputs(context.Context) (domain.PositionSnapshot, error)
}

type Outputs interface {
	SetEntryLight(context.Context, bool) error
	SetExitLight(context.Context, bool) error
	SetBuzzer(context.Context, bool) error
	RequestEntryBarrier(context.Context, bool) error
	RequestExitBarrier(context.Context, bool) error
}

type RFID interface {
	Latest() domain.RFIDObservation
}

type LPR interface {
	Latest() domain.LPRObservation
}

type VehicleRegistry interface {
	ResolveRFID(context.Context, string) (domain.VehicleIdentity, bool, error)
}

// TicketStore is retained for the current one-pass engine during migration.
// The authoritative two-pass business path uses CycleStore and only creates a
// final ticket in CompleteCycle after a valid A->B / B->A pair.
type TicketStore interface {
	Commit(context.Context, domain.Ticket) error
}

type CycleStore interface {
	OpenCycle(context.Context, domain.WeighCycle) error
	CallCycle(context.Context, string, time.Time) error
	GetCycle(context.Context, string) (domain.WeighCycle, bool, error)
	FindCalledCycle(context.Context, string, string) (domain.WeighCycle, bool, error)
	ListQueue(context.Context) ([]domain.WeighCycle, error)
	CompleteCycle(context.Context, domain.WeighCycle, domain.Ticket) error
	MarkCycleStatus(context.Context, string, domain.CycleStatus, string, time.Time) error
}

type CentralSync interface {
	PushTicket(context.Context, domain.Ticket) error
	Heartbeat(context.Context, map[string]any) error
}

type Buzzer interface {
	Pulse(context.Context, time.Duration) error
}
