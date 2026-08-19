package modbustcp

import (
	"context"
	"fmt"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
)

type Mapping struct {
	EntryPresent       uint16
	FrontPresent       uint16
	RearPresent        uint16
	ExitPresent        uint16
	EntryBarrierOpen   uint16
	EntryBarrierClosed uint16
	ExitBarrierOpen    uint16
	ExitBarrierClosed  uint16

	// SafetyClear is optional in the adapter configuration but fail-safe in behavior:
	// if it is not configured, PositionSnapshot.SafetyClear remains false.
	SafetyClear *uint16

	EntryGreen          uint16
	ExitGreen           uint16
	Buzzer              uint16
	EntryBarrierOpenCmd uint16
	ExitBarrierOpenCmd  uint16
}

type IO struct {
	Client  *Client
	Mapping Mapping
}

func (io *IO) ReadInputs(ctx context.Context) (domain.PositionSnapshot, error) {
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
	values := make([]bool, len(addresses))
	for i, addr := range addresses {
		bits, err := io.Client.ReadDiscreteInputs(ctx, addr, 1)
		if err != nil {
			return domain.PositionSnapshot{}, fmt.Errorf("read DI %d: %w", addr, err)
		}
		values[i] = bits[0]
	}

	safetyClear := false
	if io.Mapping.SafetyClear != nil {
		bits, err := io.Client.ReadDiscreteInputs(ctx, *io.Mapping.SafetyClear, 1)
		if err != nil {
			return domain.PositionSnapshot{}, fmt.Errorf("read physical safety DI %d: %w", *io.Mapping.SafetyClear, err)
		}
		safetyClear = bits[0]
	}

	return domain.PositionSnapshot{
		EntryPresent:       values[0],
		FrontPresent:       values[1],
		RearPresent:        values[2],
		ExitPresent:        values[3],
		EntryBarrierOpen:   values[4],
		EntryBarrierClosed: values[5],
		ExitBarrierOpen:    values[6],
		ExitBarrierClosed:  values[7],
		SafetyClear:        safetyClear,
		Observed:           time.Now().UTC(),
	}, nil
}

func (io *IO) SetEntryLight(ctx context.Context, green bool) error {
	return io.Client.WriteSingleCoil(ctx, io.Mapping.EntryGreen, green)
}

func (io *IO) SetExitLight(ctx context.Context, green bool) error {
	return io.Client.WriteSingleCoil(ctx, io.Mapping.ExitGreen, green)
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
