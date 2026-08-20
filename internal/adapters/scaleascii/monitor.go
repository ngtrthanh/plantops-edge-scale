package scaleascii

import (
	"sync"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
)

type Status struct {
	Enabled       bool                `json:"enabled"`
	Address       string              `json:"address,omitempty"`
	Connected     bool                `json:"connected"`
	LastReading   *domain.ScaleReading `json:"last_reading,omitempty"`
	LastFrameAtUTC *time.Time          `json:"last_frame_at_utc,omitempty"`
	LastError     string              `json:"last_error,omitempty"`
	LastErrorAtUTC *time.Time          `json:"last_error_at_utc,omitempty"`
}

type Monitor struct {
	mu     sync.RWMutex
	status Status
}

func NewMonitor(enabled bool, address string) *Monitor {
	return &Monitor{status: Status{Enabled: enabled, Address: address}}
}

func (m *Monitor) Reading(r domain.ScaleReading) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rCopy := r
	at := r.Observed
	m.status.Connected = true
	m.status.LastReading = &rCopy
	m.status.LastFrameAtUTC = &at
	m.status.LastError = ""
}

func (m *Monitor) Fault(err error) {
	if err == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	m.status.Connected = false
	m.status.LastError = err.Error()
	m.status.LastErrorAtUTC = &now
}

func (m *Monitor) Snapshot() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := m.status
	if m.status.LastReading != nil {
		r := *m.status.LastReading
		out.LastReading = &r
	}
	if m.status.LastFrameAtUTC != nil {
		t := *m.status.LastFrameAtUTC
		out.LastFrameAtUTC = &t
	}
	if m.status.LastErrorAtUTC != nil {
		t := *m.status.LastErrorAtUTC
		out.LastErrorAtUTC = &t
	}
	return out
}
