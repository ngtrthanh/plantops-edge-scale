package domain

import "time"

type RawWeightEventKind string

const (
	RawWeightFrame          RawWeightEventKind = "FRAME"
	RawWeightTransportError RawWeightEventKind = "TRANSPORT_ERROR"
)

// RawWeightRef is the immutable pointer from derived business facts back to
// the exact station-level raw-weight journal record that produced them.
type RawWeightRef struct {
	Seq  uint64 `json:"seq"`
	Hash string `json:"hash"`
}

// AuditedScaleReading is the only scale observation the workflow engine should
// consume. The RawRef proves that the exact controller frame was durably
// appended before the reading became eligible for business logic.
type AuditedScaleReading struct {
	Reading ScaleReading  `json:"reading"`
	RawRef  RawWeightRef `json:"raw_ref"`
}

// RawWeightEvent is the append-only audit representation of every scale input
// observation before business logic consumes it. RawBase64 preserves the exact
// bytes received from the controller; RawText is convenience-only for ASCII
// protocols and must never be treated as more authoritative than RawBase64.
type RawWeightEvent struct {
	StationID     string             `json:"station_id,omitempty"`
	TransactionID string             `json:"transaction_id,omitempty"`
	Kind          RawWeightEventKind `json:"kind"`
	ReceivedAtUTC time.Time          `json:"received_at_utc"`
	Source        string             `json:"source"`
	RawBase64     string             `json:"raw_base64,omitempty"`
	RawText       string             `json:"raw_text,omitempty"`
	WeightKG      *int64             `json:"weight_kg,omitempty"`
	Stable        *bool              `json:"stable,omitempty"`
	Overload      *bool              `json:"overload,omitempty"`
	Fault         string             `json:"fault,omitempty"`
	Health        Health             `json:"health"`
	ParseOK       bool               `json:"parse_ok"`
	Error         string             `json:"error,omitempty"`
}
