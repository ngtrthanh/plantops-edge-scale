package domain

import "time"

type WorkflowState string

const (
	StateIdle             WorkflowState = "IDLE"
	StateApproach         WorkflowState = "APPROACH"
	StateIdentifying      WorkflowState = "IDENTIFYING"
	StateIdentityMismatch WorkflowState = "IDENTITY_MISMATCH"
	StateQueueMismatch    WorkflowState = "QUEUE_MISMATCH"
	StateEntryAuthorized  WorkflowState = "ENTRY_AUTHORIZED"
	StateEntering         WorkflowState = "ENTERING"
	StatePositioning      WorkflowState = "POSITIONING"
	StateReadyToWeigh     WorkflowState = "READY_TO_WEIGH"
	StateWeighing         WorkflowState = "WEIGHING"
	StatePairInvalid      WorkflowState = "PAIR_INVALID"
	StateLocalCommitted   WorkflowState = "LOCAL_COMMITTED"
	StateExitAuthorized   WorkflowState = "EXIT_AUTHORIZED"
	StateExiting          WorkflowState = "EXITING"
	StateComplete         WorkflowState = "COMPLETE"
	StateFaultLockout     WorkflowState = "FAULT_LOCKOUT"
)

type IdentityStatus string
const (
	IdentityPending IdentityStatus = "PENDING"
	IdentityAccepted IdentityStatus = "ACCEPTED"
	IdentityMismatch IdentityStatus = "MISMATCH"
	IdentityManual IdentityStatus = "MANUAL_REQUIRED"
)

type PositionStatus string
const (
	PositionPending PositionStatus = "PENDING"
	PositionAccepted PositionStatus = "ACCEPTED"
	PositionManual PositionStatus = "MANUAL_REQUIRED"
)

const (
	EvidencePositionConfirmed = "POSITION_CONFIRMED"
	EvidenceIdentityConfirmed = "IDENTITY_CONFIRMED"
	EvidenceExitClear = "EXIT_CLEAR_CONFIRMED"
)

type VehicleIdentity struct { RFIDTag string `json:"rfid_tag"`; Plate string `json:"plate"` }

type WeightAcceptance struct {
	WeightKG int64 `json:"weight_kg"`
	ObservedAt time.Time `json:"observed_at"`
	RawRef RawWeightRef `json:"raw_ref"`
}

type DesiredOutputs struct {
	EntryGreen bool `json:"entry_green"`
	ExitGreen bool `json:"exit_green"`
	Buzzer bool `json:"buzzer"`
	EntryBarrierOpen bool `json:"entry_barrier_open"`
	ExitBarrierOpen bool `json:"exit_barrier_open"`
}

// Transaction is one short physical pass. Business completion requires the
// durable two-pass WeighCycle pair.
type Transaction struct {
	ID string `json:"id"`
	StationID string `json:"station_id"`
	State WorkflowState `json:"state"`
	Mode Mode `json:"mode"`
	Direction Direction `json:"direction"`
	PassNumber PassNumber `json:"pass_number"`
	CycleID string `json:"cycle_id,omitempty"`
	CycleStatus CycleStatus `json:"cycle_status,omitempty"`
	BusinessComplete bool `json:"business_complete"`
	GrossKG int64 `json:"gross_kg,omitempty"`
	TareKG int64 `json:"tare_kg,omitempty"`
	NetKG int64 `json:"net_kg,omitempty"`
	PairElapsedSeconds int64 `json:"pair_elapsed_seconds,omitempty"`
	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
	RFID RFIDObservation `json:"rfid"`
	LPR LPRObservation `json:"lpr"`
	CameraEvidence []CameraEvidence `json:"camera_evidence,omitempty"`
	Identity IdentityStatus `json:"identity"`
	IdentityReason string `json:"identity_reason,omitempty"`
	Position PositionStatus `json:"position"`
	PositionSnapshot PositionSnapshot `json:"position_snapshot"`
	LatestScale *AuditedScaleReading `json:"latest_scale,omitempty"`
	AcceptedWeight *WeightAcceptance `json:"accepted_weight,omitempty"`
	StableConfirmations int `json:"stable_confirmations"`
	Faults []Fault `json:"faults,omitempty"`
	Overrides []Override `json:"overrides,omitempty"`
	TicketID string `json:"ticket_id,omitempty"`
	LocalCommittedAt *time.Time `json:"local_committed_at,omitempty"`
	ExitSeen bool `json:"exit_seen"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	LastBlockReason string `json:"last_block_reason,omitempty"`
	Outputs DesiredOutputs `json:"outputs"`
}
