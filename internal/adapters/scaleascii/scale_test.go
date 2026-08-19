package scaleascii

import "testing"

func TestParseLine(t *testing.T) {
	r, err := ParseLine("WT=28460;ST=1;OVERLOAD=0;FAULT=\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if r.WeightKG != 28460 || !r.Stable || r.Overload {
		t.Fatalf("bad reading: %+v", r)
	}
}
