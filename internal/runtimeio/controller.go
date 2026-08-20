package runtimeio

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/engine"
)

type Workflow interface {
	Snapshot() engine.Snapshot
	ObservePosition(context.Context, domain.PositionSnapshot) error
	ObserveFault(context.Context, domain.Fault) error
	ClearFault(context.Context, domain.DeviceID) error
}

type Inputs interface {
	ReadInputs(context.Context) (domain.PositionSnapshot, error)
}

type Outputs interface {
	SetEntryLight(context.Context, bool) error
	SetExitLight(context.Context, bool) error
	SetBuzzer(context.Context, bool) error
	RequestEntryBarrier(context.Context, bool) error
	RequestExitBarrier(context.Context, bool) error
	SafeState(context.Context) error
}

type Status struct {
	Enabled       bool                    `json:"enabled"`
	Connected     bool                    `json:"connected"`
	LastInput     domain.PositionSnapshot `json:"last_input"`
	Desired       domain.DesiredOutputs   `json:"desired"`
	Applied       domain.DesiredOutputs   `json:"applied"`
	LastSuccessAt time.Time               `json:"last_success_at,omitempty"`
	LastErrorAt   time.Time               `json:"last_error_at,omitempty"`
	LastError     string                  `json:"last_error,omitempty"`
}

type Monitor struct {
	mu sync.RWMutex
	v  Status
}

func NewMonitor(enabled bool) *Monitor { return &Monitor{v: Status{Enabled: enabled}} }
func (m *Monitor) Snapshot() Status { m.mu.RLock(); defer m.mu.RUnlock(); return m.v }
func (m *Monitor) input(p domain.PositionSnapshot) {
	m.mu.Lock(); defer m.mu.Unlock()
	m.v.Connected = true; m.v.LastInput = p; m.v.LastSuccessAt = time.Now().UTC(); m.v.LastError = ""
}
func (m *Monitor) desired(v domain.DesiredOutputs) { m.mu.Lock(); defer m.mu.Unlock(); m.v.Desired = v }
func (m *Monitor) applied(v domain.DesiredOutputs) { m.mu.Lock(); defer m.mu.Unlock(); m.v.Applied = v; m.v.LastSuccessAt = time.Now().UTC() }
func (m *Monitor) fault(err error) {
	m.mu.Lock(); defer m.mu.Unlock()
	m.v.Connected = false; m.v.LastErrorAt = time.Now().UTC()
	if err != nil { m.v.LastError = err.Error() }
}

type Controller struct {
	Workflow Workflow
	Inputs   Inputs
	Outputs  Outputs
	Monitor  *Monitor

	PollInterval           time.Duration
	BuzzerPulse            time.Duration
	BarrierFeedbackTimeout time.Duration

	applied         domain.DesiredOutputs
	lastBuzzerReady bool
	buzzerUntil     time.Time
	entryOpenSince  time.Time
	exitOpenSince   time.Time
	safeApplied     bool
}

func (c *Controller) Run(ctx context.Context) error {
	if c.Workflow == nil || c.Inputs == nil || c.Outputs == nil {
		return errors.New("runtime I/O requires workflow, inputs and outputs")
	}
	if c.Monitor == nil { c.Monitor = NewMonitor(true) }

	ticker := time.NewTicker(c.pollInterval())
	defer ticker.Stop()
	for {
		_ = c.step(ctx) // faults are reported into workflow/monitor and retried
		select {
		case <-ctx.Done(): return nil
		case <-ticker.C:
		}
	}
}

func (c *Controller) step(ctx context.Context) error {
	p, err := c.Inputs.ReadInputs(ctx)
	if err != nil {
		c.safeApplied = false
		c.Monitor.fault(err)
		_ = c.Workflow.ObserveFault(ctx, domain.Fault{
			Device: domain.DeviceRemoteIO, Health: domain.HealthDisconnected,
			Reason: err.Error(), Overridable: false, Critical: true,
		})
		return err
	}
	c.Monitor.input(p)

	if !c.safeApplied {
		if err := c.Outputs.SafeState(ctx); err != nil {
			c.Monitor.fault(err)
			_ = c.Workflow.ObserveFault(ctx, domain.Fault{
				Device: domain.DeviceRemoteIO, Health: domain.HealthFault,
				Reason: "cannot establish safe output state: " + err.Error(), Overridable: false, Critical: true,
			})
			return err
		}
		c.applied = domain.DesiredOutputs{}
		c.lastBuzzerReady = false
		c.buzzerUntil = time.Time{}
		c.entryOpenSince = time.Time{}
		c.exitOpenSince = time.Time{}
		c.safeApplied = true
		c.Monitor.applied(c.applied)
	}

	_ = c.Workflow.ClearFault(ctx, domain.DeviceRemoteIO)
	if err := c.Workflow.ObservePosition(ctx, p); err != nil { return err }

	entryContradiction := p.EntryBarrierOpen && p.EntryBarrierClosed
	exitContradiction := p.ExitBarrierOpen && p.ExitBarrierClosed
	if entryContradiction || exitContradiction {
		if entryContradiction { c.barrierFault(ctx, domain.DeviceEntryBarrier, "contradictory entry barrier OPEN+CLOSED feedback") }
		if exitContradiction { c.barrierFault(ctx, domain.DeviceExitBarrier, "contradictory exit barrier OPEN+CLOSED feedback") }
		// Drop every software permissive request. Do not continue into reconcile,
		// otherwise an OPEN bit could clear the contradiction in this same poll.
		if err := c.Outputs.SafeState(ctx); err != nil { return c.ioFault(ctx, err) }
		c.applied = domain.DesiredOutputs{}
		c.lastBuzzerReady = false
		c.buzzerUntil = time.Time{}
		c.entryOpenSince = time.Time{}
		c.exitOpenSince = time.Time{}
		c.Monitor.applied(c.applied)
		return nil
	}

	snap := c.Workflow.Snapshot()
	desired := domain.DesiredOutputs{}
	if snap.Transaction != nil { desired = snap.Transaction.Outputs }
	c.Monitor.desired(desired)
	return c.reconcile(ctx, desired, p)
}

func (c *Controller) reconcile(ctx context.Context, desired domain.DesiredOutputs, p domain.PositionSnapshot) error {
	now := time.Now().UTC()

	// Apply barrier requests before permissive GREEN. When removing an OPEN
	// request, remove GREEN first.
	if desired.EntryBarrierOpen != c.applied.EntryBarrierOpen {
		if !desired.EntryBarrierOpen && c.applied.EntryGreen {
			if err := c.Outputs.SetEntryLight(ctx, false); err != nil { return c.ioFault(ctx, err) }
			c.applied.EntryGreen = false
		}
		if err := c.Outputs.RequestEntryBarrier(ctx, desired.EntryBarrierOpen); err != nil { return c.ioFault(ctx, err) }
		c.applied.EntryBarrierOpen = desired.EntryBarrierOpen
		if desired.EntryBarrierOpen { c.entryOpenSince = now } else { c.entryOpenSince = time.Time{} }
	}
	if desired.ExitBarrierOpen != c.applied.ExitBarrierOpen {
		if !desired.ExitBarrierOpen && c.applied.ExitGreen {
			if err := c.Outputs.SetExitLight(ctx, false); err != nil { return c.ioFault(ctx, err) }
			c.applied.ExitGreen = false
		}
		if err := c.Outputs.RequestExitBarrier(ctx, desired.ExitBarrierOpen); err != nil { return c.ioFault(ctx, err) }
		c.applied.ExitBarrierOpen = desired.ExitBarrierOpen
		if desired.ExitBarrierOpen { c.exitOpenSince = now } else { c.exitOpenSince = time.Time{} }
	}

	// A permissive signal requires physical OPEN feedback, not only domain
	// authorization.
	entryGreen := desired.EntryGreen && (!desired.EntryBarrierOpen || p.EntryBarrierOpen)
	exitGreen := desired.ExitGreen && (!desired.ExitBarrierOpen || p.ExitBarrierOpen)
	if entryGreen != c.applied.EntryGreen {
		if err := c.Outputs.SetEntryLight(ctx, entryGreen); err != nil { return c.ioFault(ctx, err) }
		c.applied.EntryGreen = entryGreen
	}
	if exitGreen != c.applied.ExitGreen {
		if err := c.Outputs.SetExitLight(ctx, exitGreen); err != nil { return c.ioFault(ctx, err) }
		c.applied.ExitGreen = exitGreen
	}

	// Release buzzer is also feedback-gated. It pulses only when the exit boom
	// is confirmed open, never merely because the engine requested OPEN.
	buzzerReady := desired.Buzzer && (!desired.ExitBarrierOpen || p.ExitBarrierOpen)
	if buzzerReady && !c.lastBuzzerReady { c.buzzerUntil = now.Add(c.buzzerPulse()) }
	if !buzzerReady { c.buzzerUntil = time.Time{} }
	c.lastBuzzerReady = buzzerReady
	buzzerOn := !c.buzzerUntil.IsZero() && now.Before(c.buzzerUntil)
	if buzzerOn != c.applied.Buzzer {
		if err := c.Outputs.SetBuzzer(ctx, buzzerOn); err != nil { return c.ioFault(ctx, err) }
		c.applied.Buzzer = buzzerOn
	}

	if desired.EntryBarrierOpen {
		if p.EntryBarrierOpen {
			c.entryOpenSince = time.Time{}
			_ = c.Workflow.ClearFault(ctx, domain.DeviceEntryBarrier)
		} else if !c.entryOpenSince.IsZero() && now.Sub(c.entryOpenSince) > c.barrierTimeout() {
			c.barrierFault(ctx, domain.DeviceEntryBarrier, "entry barrier did not confirm OPEN before timeout")
		}
	}
	if desired.ExitBarrierOpen {
		if p.ExitBarrierOpen {
			c.exitOpenSince = time.Time{}
			_ = c.Workflow.ClearFault(ctx, domain.DeviceExitBarrier)
		} else if !c.exitOpenSince.IsZero() && now.Sub(c.exitOpenSince) > c.barrierTimeout() {
			c.barrierFault(ctx, domain.DeviceExitBarrier, "exit barrier did not confirm OPEN before timeout")
		}
	}

	c.Monitor.applied(c.applied)
	return nil
}

func (c *Controller) ioFault(ctx context.Context, err error) error {
	if err == nil { return nil }
	c.safeApplied = false
	c.Monitor.fault(err)
	_ = c.Workflow.ObserveFault(ctx, domain.Fault{
		Device: domain.DeviceRemoteIO, Health: domain.HealthFault,
		Reason: err.Error(), Overridable: false, Critical: true,
	})
	return err
}

func (c *Controller) barrierFault(ctx context.Context, device domain.DeviceID, reason string) {
	_ = c.Workflow.ObserveFault(ctx, domain.Fault{
		Device: device, Health: domain.HealthFault, Reason: reason,
		Overridable: false, Critical: true,
	})
}

func (c *Controller) pollInterval() time.Duration {
	if c.PollInterval <= 0 { return 100 * time.Millisecond }
	return c.PollInterval
}
func (c *Controller) buzzerPulse() time.Duration {
	if c.BuzzerPulse <= 0 { return 700 * time.Millisecond }
	return c.BuzzerPulse
}
func (c *Controller) barrierTimeout() time.Duration {
	if c.BarrierFeedbackTimeout <= 0 { return 5 * time.Second }
	return c.BarrierFeedbackTimeout
}

func (c *Controller) String() string {
	return fmt.Sprintf("poll=%s buzzer=%s barrier_timeout=%s", c.pollInterval(), c.buzzerPulse(), c.barrierTimeout())
}
