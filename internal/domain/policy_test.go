package domain

import "testing"

func healthyScale() ScaleReading { return ScaleReading{Health: HealthHealthy, Stable: true, WeightKG: 28460} }

func TestScaleFaultLocksOut(t *testing.T) {
	d := EvaluateMode(ScaleReading{Health: HealthDisconnected}, nil)
	if d.Mode != ModeLockout {
		t.Fatalf("got %s", d.Mode)
	}
}

func TestOneAuxFaultDegrades(t *testing.T) {
	d := EvaluateMode(healthyScale(), []Fault{{Device: DeviceFrontSensor, Health: HealthFault, Overridable: true}})
	if d.Mode != ModeDegraded || d.RequiredRole != RoleOperator {
		t.Fatalf("got %+v", d)
	}
}

func TestMultipleAuxFaultsRequireManualSupervisor(t *testing.T) {
	d := EvaluateMode(healthyScale(), []Fault{
		{Device: DeviceRFID, Health: HealthFault, Overridable: true},
		{Device: DeviceLPR, Health: HealthFault, Overridable: true},
	})
	if d.Mode != ModeManual || d.RequiredRole != RoleSupervisor {
		t.Fatalf("got %+v", d)
	}
}

func TestScaleOverrideRejected(t *testing.T) {
	err := ValidateOverride(Override{TransactionID: "tx1", Device: DeviceScale, Reason: "bad", AuthorizedBy: "sup", Evidence: []string{"manual"}})
	if err == nil {
		t.Fatal("expected scale override rejection")
	}
}
