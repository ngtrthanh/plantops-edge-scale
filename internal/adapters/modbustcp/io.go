package modbustcp

import (
	"context"
	"fmt"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
)

type IO struct {
	Client  *Client
	Mapping Mapping
}

func (io *IO) ReadInputs(ctx context.Context) (domain.PositionSnapshot, error) {
	if io.Client == nil {
		return domain.PositionSnapshot{}, fmt.Errorf("Modbus client is nil")
	}

	addresses := []uint16{
		io.Mapping.EntryPresent,
		io.Mapping.FrontPresent,
		io.Mapping.RearPresent,
		io.Mapping.ExitPresent,
		io.Mapping.EntryBarrierOpen,
		io.Mapping.EntryBarrierClosed,
		io.Mapping.ExitBarrierOpen,
		io.Mapping.ExitBarrierClosed,
	}
	if io.Mapping.SafetyClear != nil {
		addresses = append(addresses, *io.Mapping.SafetyClear)
	}
	minAddr, maxAddr := addresses[0], addresses[0]
	for _, addr := range addresses[1:] {
		if addr < minAddr { minAddr = addr }
		if addr > maxAddr { maxAddr = addr }
	}
	bits, err := io.Client.ReadDiscreteInputs(ctx, minAddr, maxAddr-minAddr+1)
	if err != nil {
		return domain.PositionSnapshot{}, fmt.Errorf("read DI block %d..%d: %w", minAddr, maxAddr, err)
	}
	get := func(addr uint16) bool { return bits[int(addr-minAddr)] }

	safetyClear := false
	if io.Mapping.SafetyClear != nil {
		safetyClear = get(*io.Mapping.SafetyClear)
	}
	return domain.PositionSnapshot{
		EntryPresent:       get(io.Mapping.EntryPresent),
		FrontPresent:       get(io.Mapping.FrontPresent),
		RearPresent:        get(io.Mapping.RearPresent),
		ExitPresent:        get(io.Mapping.ExitPresent),
		EntryBarrierOpen:   get(io.Mapping.EntryBarrierOpen),
		EntryBarrierClosed: get(io.Mapping.EntryBarrierClosed),
		ExitBarrierOpen:    get(io.Mapping.ExitBarrierOpen),
		ExitBarrierClosed:  get(io.Mapping.ExitBarrierClosed),
		SafetyClear:        safetyClear,
		Observed:           time.Now().UTC(),
	}, nil
}

// SetEntryLight drives a mutually exclusive RED/GREEN pair. Ordering is
// fail-safe: the currently permissive GREEN is always removed before RED is
// asserted; when granting GREEN, RED is removed first so a partial failure
// leaves the signal dark rather than falsely permissive.
func (io *IO) SetEntryLight(ctx context.Context, green bool) error {
	return io.setSignal(ctx, io.Mapping.EntryRed, io.Mapping.EntryGreen, green)
}

func (io *IO) SetExitLight(ctx context.Context, green bool) error {
	return io.setSignal(ctx, io.Mapping.ExitRed, io.Mapping.ExitGreen, green)
}

func (io *IO) setSignal(ctx context.Context, redAddr, greenAddr uint16, green bool) error {
	if green {
		if err := io.Client.WriteSingleCoil(ctx, redAddr, false); err != nil { return err }
		return io.Client.WriteSingleCoil(ctx, greenAddr, true)
	}
	if err := io.Client.WriteSingleCoil(ctx, greenAddr, false); err != nil { return err }
	return io.Client.WriteSingleCoil(ctx, redAddr, true)
}

func (io *IO) SetBuzzer(ctx context.Context, on bool) error {
	return io.Client.WriteSingleCoil(ctx, io.Mapping.Buzzer, on)
}

func (io *IO) RequestEntryBarrier(ctx context.Context, open bool) error {
	return io.Client.WriteSingleCoil(ctx, io.Mapping.EntryBarrierOpenCmd, open)
}

func (io *IO) RequestExitBarrier(ctx context.Context, open bool) error {
	return io.Client.WriteSingleCoil(ctx, io.Mapping.ExitBarrierOpenCmd, open)
}

// SafeState is called at startup/reconnect before any workflow command is
// honored. It removes permissive outputs and open requests, while showing RED.
func (io *IO) SafeState(ctx context.Context) error {
	if err := io.SetBuzzer(ctx, false); err != nil { return fmt.Errorf("buzzer safe: %w", err) }
	if err := io.SetEntryLight(ctx, false); err != nil { return fmt.Errorf("entry signal safe: %w", err) }
	if err := io.SetExitLight(ctx, false); err != nil { return fmt.Errorf("exit signal safe: %w", err) }
	if err := io.RequestEntryBarrier(ctx, false); err != nil { return fmt.Errorf("entry barrier safe: %w", err) }
	if err := io.RequestExitBarrier(ctx, false); err != nil { return fmt.Errorf("exit barrier safe: %w", err) }
	return nil
}
