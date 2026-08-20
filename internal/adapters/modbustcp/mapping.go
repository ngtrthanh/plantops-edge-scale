package modbustcp

import (
	"fmt"
	"strconv"
	"strings"
)

// Mapping is the logical-to-Modbus address map. DI and coil address spaces are
// independent, so both may start at zero. SafetyClear is optional and remains
// fail-safe false when not configured.
type Mapping struct {
	EntryPresent       uint16
	FrontPresent       uint16
	RearPresent        uint16
	ExitPresent        uint16
	EntryBarrierOpen   uint16
	EntryBarrierClosed uint16
	ExitBarrierOpen    uint16
	ExitBarrierClosed  uint16
	SafetyClear        *uint16

	EntryRed            uint16
	EntryGreen          uint16
	ExitRed             uint16
	ExitGreen           uint16
	Buzzer              uint16
	EntryBarrierOpenCmd uint16
	ExitBarrierOpenCmd  uint16
}

func DefaultMapping() Mapping {
	return Mapping{
		EntryPresent: 0, FrontPresent: 1, RearPresent: 2, ExitPresent: 3,
		EntryBarrierOpen: 4, EntryBarrierClosed: 5,
		ExitBarrierOpen: 6, ExitBarrierClosed: 7,
		SafetyClear: nil,
		EntryRed: 0, EntryGreen: 1, ExitRed: 2, ExitGreen: 3,
		Buzzer: 4, EntryBarrierOpenCmd: 5, ExitBarrierOpenCmd: 6,
	}
}

// ParseMapping applies comma-separated key=address overrides on top of the
// documented default map. Example:
//
//   entry_present=0,front_present=1,safety_clear=8,entry_red=0
//
// Use safety_clear=none to explicitly disable that input.
func ParseMapping(spec string) (Mapping, error) {
	m := DefaultMapping()
	if strings.TrimSpace(spec) == "" {
		return m, nil
	}
	for _, item := range strings.Split(spec, ",") {
		kv := strings.SplitN(strings.TrimSpace(item), "=", 2)
		if len(kv) != 2 {
			return Mapping{}, fmt.Errorf("invalid I/O mapping item %q", item)
		}
		key := strings.ToLower(strings.TrimSpace(kv[0]))
		value := strings.TrimSpace(kv[1])
		if key == "safety_clear" && strings.EqualFold(value, "none") {
			m.SafetyClear = nil
			continue
		}
		n, err := strconv.ParseUint(value, 10, 16)
		if err != nil {
			return Mapping{}, fmt.Errorf("invalid address for %s: %w", key, err)
		}
		addr := uint16(n)
		switch key {
		case "entry_present": m.EntryPresent = addr
		case "front_present": m.FrontPresent = addr
		case "rear_present": m.RearPresent = addr
		case "exit_present": m.ExitPresent = addr
		case "entry_barrier_open_fb": m.EntryBarrierOpen = addr
		case "entry_barrier_closed_fb": m.EntryBarrierClosed = addr
		case "exit_barrier_open_fb": m.ExitBarrierOpen = addr
		case "exit_barrier_closed_fb": m.ExitBarrierClosed = addr
		case "safety_clear": m.SafetyClear = &addr
		case "entry_red": m.EntryRed = addr
		case "entry_green": m.EntryGreen = addr
		case "exit_red": m.ExitRed = addr
		case "exit_green": m.ExitGreen = addr
		case "buzzer": m.Buzzer = addr
		case "entry_barrier_open_cmd": m.EntryBarrierOpenCmd = addr
		case "exit_barrier_open_cmd": m.ExitBarrierOpenCmd = addr
		default:
			return Mapping{}, fmt.Errorf("unknown I/O mapping key %q", key)
		}
	}
	return m, nil
}
