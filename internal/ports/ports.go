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

type TicketStore interface {
	Commit(context.Context, domain.Ticket) error
}

type CentralSync interface {
	PushTicket(context.Context, domain.Ticket) error
	Heartbeat(context.Context, map[string]any) error
}

type Buzzer interface {
	Pulse(context.Context, time.Duration) error
}
