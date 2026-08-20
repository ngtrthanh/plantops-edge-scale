package domain

import "time"

type AuditKind string

const (
	AuditTransactionStarted AuditKind = "TRANSACTION_STARTED"
	AuditStateTransition    AuditKind = "STATE_TRANSITION"
	AuditRFIDObserved       AuditKind = "RFID_OBSERVED"
	AuditLPRObserved        AuditKind = "LPR_OBSERVED"
	AuditIdentityDecision   AuditKind = "IDENTITY_DECISION"
	AuditPositionDecision   AuditKind = "POSITION_DECISION"
	AuditFaultSet           AuditKind = "FAULT_SET"
	AuditFaultCleared       AuditKind = "FAULT_CLEARED"
	AuditOverrideAuthorized AuditKind = "OVERRIDE_AUTHORIZED"
	AuditStableAccepted     AuditKind = "STABLE_WEIGHT_ACCEPTED"
	AuditTicketCommitted    AuditKind = "TICKET_COMMITTED"
	AuditDesiredOutputs     AuditKind = "DESIRED_OUTPUTS_CHANGED"
	AuditOutputCommand      AuditKind = "OUTPUT_COMMAND"
	AuditOutputResult       AuditKind = "OUTPUT_RESULT"
	AuditBarrierFeedback    AuditKind = "BARRIER_FEEDBACK"
	AuditTransactionDone    AuditKind = "TRANSACTION_COMPLETED"
	AuditTransactionReset   AuditKind = "TRANSACTION_RESET"
)

type AuditRef struct {
	Seq  uint64 `json:"seq"`
	Hash string `json:"hash"`
}

// AuditEvent is the low-volume operational/business audit stream. It is
// intentionally separate from RawWeightEvent: raw weight keeps every controller
// frame, while this stream records meaningful decisions, actions and changes.
type AuditEvent struct {
	StationID     string         `json:"station_id"`
	TransactionID string         `json:"transaction_id,omitempty"`
	AtUTC         time.Time      `json:"at_utc"`
	Kind          AuditKind      `json:"kind"`
	Actor         string         `json:"actor,omitempty"`
	Source        string         `json:"source"`
	Device        DeviceID       `json:"device,omitempty"`
	Action        string         `json:"action,omitempty"`
	OldState      WorkflowState  `json:"old_state,omitempty"`
	NewState      WorkflowState  `json:"new_state,omitempty"`
	Reason        string         `json:"reason,omitempty"`
	Evidence      []string       `json:"evidence,omitempty"`
	Data          map[string]any `json:"data,omitempty"`
	RuntimeGitSHA string         `json:"runtime_git_sha,omitempty"`
}
