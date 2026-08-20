package modbustcp

import "testing"

func TestParseMappingOverridesDocumentedDefaults(t *testing.T) {
	m, err := ParseMapping("front_present=11,safety_clear=18,entry_red=20,exit_barrier_open_cmd=26")
	if err != nil { t.Fatal(err) }
	if m.EntryPresent != 0 || m.FrontPresent != 11 { t.Fatalf("unexpected DI map: %+v", m) }
	if m.SafetyClear == nil || *m.SafetyClear != 18 { t.Fatalf("safety_clear=%v", m.SafetyClear) }
	if m.EntryRed != 20 || m.ExitBarrierOpenCmd != 26 { t.Fatalf("unexpected DO map: %+v", m) }
}

func TestParseMappingRejectsUnknownKey(t *testing.T) {
	if _, err := ParseMapping("magic_sensor=1"); err == nil { t.Fatal("expected unknown mapping key error") }
}

func TestSafetyClearDefaultsFailSafeUnconfigured(t *testing.T) {
	m := DefaultMapping()
	if m.SafetyClear != nil { t.Fatalf("safety clear must default unconfigured, got %v", *m.SafetyClear) }
}
