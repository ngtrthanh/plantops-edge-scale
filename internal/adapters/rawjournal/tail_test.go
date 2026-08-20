package rawjournal

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
)

func TestTailReturnsNewestRecordsInChronologicalOrder(t *testing.T) {
	j := &Journal{Path: filepath.Join(t.TempDir(), "raw-weight.jsonl")}
	for i := int64(1); i <= 5; i++ {
		w := i * 1000
		stable := i == 5
		if err := j.Append(context.Background(), domain.RawWeightEvent{
			StationID: "S01", Kind: domain.RawWeightFrame,
			ReceivedAtUTC: time.Unix(i, 0).UTC(), Source: "test",
			WeightKG: &w, Stable: &stable, Health: domain.HealthHealthy, ParseOK: true,
		}); err != nil {
			t.Fatal(err)
		}
	}

	records, err := j.Tail(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("got %d records, want 3", len(records))
	}
	want := []int64{3000, 4000, 5000}
	for i, w := range want {
		if records[i].Event.WeightKG == nil || *records[i].Event.WeightKG != w {
			t.Fatalf("record %d=%+v want weight %d", i, records[i], w)
		}
	}
	if err := j.Verify(); err != nil {
		t.Fatalf("journal verify failed: %v", err)
	}
}
