package domain

import "time"

type Mode string

const (
	ModeNormal   Mode = "NORMAL"
	ModeDegraded Mode = "DEGRADED"
	ModeManual   Mode = "MANUAL"
	ModeLockout  Mode = "FAULT_LOCKOUT"
)

type Health string

const (
	HealthHealthy      Health = "HEALTHY"
	HealthStale        Health = "STALE"
	HealthDisconnected Health = "DISCONNECTED"
	HealthFault        Health = "FAULT"
)

type DeviceID string

const (
	DeviceScale          DeviceID = "SCALE"
	DeviceRFID           DeviceID = "RFID"
	DeviceLPR            DeviceID = "LPR"
	DeviceEntrySensor    DeviceID = "ENTRY_SENSOR"
	DeviceFrontSensor    DeviceID = "FRONT_SENSOR"
	DeviceRearSensor     DeviceID = "REAR_SENSOR"
	DeviceExitSensor     DeviceID = "EXIT_SENSOR"
	DeviceEntryBarrier   DeviceID = "ENTRY_BARRIER"
	DeviceExitBarrier    DeviceID = "EXIT_BARRIER"
	DevicePhysicalSafety DeviceID = "PHYSICAL_SAFETY"
	DeviceRemoteIO       DeviceID = "REMOTE_IO"
	DeviceAuditStore     DeviceID = "AUDIT_STORE"
)

type Role string

const (
	RoleOperator   Role = "OPERATOR"
	RoleSupervisor Role = "SUPERVISOR"
)

type ScaleReading struct {
	WeightKG int64     `json:"weight_kg"`
	Stable   bool      `json:"stable"`
	Overload bool      `json:"overload"`
	Fault    string    `json:"fault,omitempty"`
	Health   Health    `json:"health"`
	Observed time.Time `json:"observed_at"`
}

type PositionSnapshot struct {
	EntryPresent       bool      `json:"entry_present"`
	FrontPresent       bool      `json:"front_present"`
	RearPresent        bool      `json:"rear_present"`
	ExitPresent        bool      `json:"exit_present"`
	EntryBarrierOpen   bool      `json:"entry_barrier_open"`
	EntryBarrierClosed bool      `json:"entry_barrier_closed"`
	ExitBarrierOpen    bool      `json:"exit_barrier_open"`
	ExitBarrierClosed  bool      `json:"exit_barrier_closed"`
	SafetyClear        bool      `json:"safety_clear"`
	Observed           time.Time `json:"observed_at"`
}

type RFIDObservation struct {
	Tag      string    `json:"tag"`
	Quality  float64   `json:"quality,omitempty"`
	Health   Health    `json:"health"`
	Observed time.Time `json:"observed_at"`
}

type LPRObservation struct {
	Plate      string    `json:"plate"`
	Confidence float64   `json:"confidence,omitempty"`
	ImageRef   string    `json:"image_ref,omitempty"`
	Health     Health    `json:"health"`
	Observed   time.Time `json:"observed_at"`
}

type Fault struct {
	Device      DeviceID `json:"device"`
	Health      Health    `json:"health"`
	Reason      string    `json:"reason"`
	Overridable bool      `json:"overridable"`
	Critical    bool      `json:"critical"`
}

type Override struct {
	TransactionID string     `json:"transaction_id"`
	Device        DeviceID   `json:"device"`
	Reason        string     `json:"reason"`
	RequestedBy   string     `json:"requested_by"`
	AuthorizedBy  string     `json:"authorized_by"`
	AuthorizedAs  Role       `json:"authorized_as"`
	AuthorizedAt  time.Time  `json:"authorized_at"`
	Evidence      []string   `json:"evidence"`
	ExpiredAt     *time.Time `json:"expired_at,omitempty"`
}

type Ticket struct {
	ID                string       `json:"id"`
	StationID         string       `json:"station_id"`
	TransactionID     string       `json:"transaction_id"`
	Plate             string       `json:"plate"`
	RFID              string       `json:"rfid"`
	WeightKG          int64        `json:"weight_kg"`
	WeightObservedAt  time.Time    `json:"weight_observed_at"`
	WeightRawRef      RawWeightRef `json:"weight_raw_ref"`
	Mode              Mode         `json:"mode"`
	Overrides         []Override   `json:"overrides,omitempty"`
	CommittedAt       time.Time    `json:"committed_at"`
	SyncedAt          *time.Time   `json:"synced_at,omitempty"`
}
