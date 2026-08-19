package domain

import (
	"errors"
	"fmt"
)

type Decision struct {
	Mode         Mode     `json:"mode"`
	Reasons      []string `json:"reasons,omitempty"`
	RequiredRole Role     `json:"required_role,omitempty"`
}

func EvaluateMode(scale ScaleReading, faults []Fault) Decision {
	if scale.Health != HealthHealthy || scale.Fault != "" || scale.Overload {
		return Decision{Mode: ModeLockout, Reasons: []string{"authoritative scale unavailable or faulted"}}
	}

	aux := make([]Fault, 0, len(faults))
	for _, f := range faults {
		if f.Device == DeviceScale {
			return Decision{Mode: ModeLockout, Reasons: []string{"scale faults are non-overridable"}}
		}
		if f.Critical || !f.Overridable {
			return Decision{Mode: ModeLockout, Reasons: []string{fmt.Sprintf("critical/non-overridable fault: %s", f.Device)}}
		}
		aux = append(aux, f)
	}

	switch len(aux) {
	case 0:
		return Decision{Mode: ModeNormal}
	case 1:
		return Decision{Mode: ModeDegraded, Reasons: []string{fmt.Sprintf("auxiliary device unavailable: %s", aux[0].Device)}, RequiredRole: RoleOperator}
	default:
		return Decision{Mode: ModeManual, Reasons: []string{"multiple auxiliary failures require supervised manual operation"}, RequiredRole: RoleSupervisor}
	}
}

func RequiredRoleForOverride(device DeviceID, simultaneousAuxFaults int) Role {
	if simultaneousAuxFaults > 1 || device == DeviceEntryBarrier || device == DeviceExitBarrier || device == DevicePhysicalSafety {
		return RoleSupervisor
	}
	return RoleOperator
}

func ValidateOverride(o Override) error {
	if o.TransactionID == "" {
		return errors.New("override must be transaction-scoped")
	}
	if o.Device == DeviceScale {
		return errors.New("scale controller/weight/stable signal cannot be overridden")
	}
	if o.Reason == "" {
		return errors.New("override reason is required")
	}
	if o.AuthorizedBy == "" {
		return errors.New("override authorization is required")
	}
	if len(o.Evidence) == 0 {
		return errors.New("fallback evidence is required")
	}
	return nil
}
