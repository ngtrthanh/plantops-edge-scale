package rawjournal

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
)

func TestJournalAppendVerifyAndResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "raw-weight.jsonl")
	j := &Journal{Path: path}
	weight := int64(28460)
	stable := true

	for i := 0; i < 2; i++ {
		e := domain.RawWeightEvent{
			StationID: "S01", Kind: domain.RawWeightFrame,
			ReceivedAtUTC: time.Date(2026, 8, 19, 5, 0, i, 0, time.UTC),
			Source: "127.0.0.1:4001", RawBase64: "V1Q9Mjg0NjA7U1Q9MQo=",
			WeightKG: &weight, Stable: &stable, Health: domain.HealthHealthy, ParseOK: true,
		}
		if err := j.Append(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	if err := Verify(path); err != nil {
		t.Fatalf("verify: %v", err)
	}

	// New Journal instance must resume the chain instead of restarting sequence.
	j2 := &Journal{Path: path}
	if err := j2.Append(context.Background(), domain.RawWeightEvent{
		StationID: "S01", Kind: domain.RawWeightTransportError,
		ReceivedAtUTC: time.Date(2026, 8, 19, 5, 0, 2, 0, time.UTC),
		Source: "127.0.0.1:4001", Health: domain.HealthDisconnected,
		Error: "connection lost",
	}); err != nil {
		t.Fatal(err)
	}
	if err := Verify(path); err != nil {
		t.Fatalf("verify after resume: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var last Record
	lines := splitNonEmptyLines(b)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	if err := json.Unmarshal(lines[2], &last); err != nil {
		t.Fatal(err)
	}
	if last.Seq != 3 || last.PrevHash == "" || last.Hash == "" {
		t.Fatalf("unexpected last record: %+v", last)
	}
}

func TestVerifyDetectsTamper(t *testing.T) {
	path := filepath.Join(t.TempDir(), "raw-weight.jsonl")
	j := &Journal{Path: path}
	weight := int64(100)
	if err := j.Append(context.Background(), domain.RawWeightEvent{
		Kind: domain.RawWeightFrame, ReceivedAtUTC: time.Now().UTC(), Source: "scale",
		WeightKG: &weight, Health: domain.HealthHealthy, ParseOK: true,
	}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := range b {
		if b[i] == '1' {
			b[i] = '2'
			break
		}
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Verify(path); err == nil {
		t.Fatal("expected tamper verification failure")
	}
}

func splitNonEmptyLines(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			if i > start {
				out = append(out, b[start:i])
			}
			start = i + 1
		}
	}
	return out
}
