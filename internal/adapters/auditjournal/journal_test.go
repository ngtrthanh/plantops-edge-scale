package auditjournal

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
)

func TestAppendTailVerifyAndResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	j := &Journal{Path: path}
	ctx := context.Background()
	for i, kind := range []domain.AuditKind{domain.AuditTransactionStarted, domain.AuditStateTransition, domain.AuditTicketCommitted} {
		ref, err := j.Append(ctx, domain.AuditEvent{
			StationID: "S01", TransactionID: "TX-1", AtUTC: time.Now().UTC(),
			Kind: kind, Source: "TEST", Data: map[string]any{"n": i},
		})
		if err != nil { t.Fatal(err) }
		if ref.Seq != uint64(i+1) || ref.Hash == "" { t.Fatalf("bad ref: %+v", ref) }
	}
	if err := j.Verify(); err != nil { t.Fatal(err) }
	rows, err := j.Tail(2)
	if err != nil { t.Fatal(err) }
	if len(rows) != 2 || rows[0].Seq != 2 || rows[1].Seq != 3 { t.Fatalf("tail=%+v", rows) }

	// A new process/journal instance must resume the chain rather than reset it.
	j2 := &Journal{Path: path}
	ref, err := j2.Append(ctx, domain.AuditEvent{StationID:"S01", AtUTC:time.Now().UTC(), Kind:domain.AuditTransactionDone, Source:"TEST"})
	if err != nil { t.Fatal(err) }
	if ref.Seq != 4 { t.Fatalf("resume seq=%d want 4", ref.Seq) }
	if err := j2.Verify(); err != nil { t.Fatal(err) }
}

func TestVerifyDetectsTamper(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	j := &Journal{Path:path}
	_, _ = j.Append(context.Background(), domain.AuditEvent{StationID:"S01", AtUTC:time.Now().UTC(), Kind:domain.AuditStateTransition, Source:"TEST", Reason:"original"})
	b, err := os.ReadFile(path)
	if err != nil { t.Fatal(err) }
	for i := range b {
		if i+8 <= len(b) && string(b[i:i+8]) == "original" { copy(b[i:i+8], []byte("tampered")); break }
	}
	if err := os.WriteFile(path,b,0o644); err != nil { t.Fatal(err) }
	if err := Verify(path); err == nil { t.Fatal("expected tamper detection") }
}
