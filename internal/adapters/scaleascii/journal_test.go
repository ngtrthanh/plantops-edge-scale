package scaleascii

import (
	"context"
	"encoding/base64"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
)

type memoryJournal struct {
	mu     sync.Mutex
	events []domain.RawWeightEvent
	err    error
}

func (m *memoryJournal) Append(_ context.Context, e domain.RawWeightEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.events = append(m.events, e)
	return nil
}

func TestReaderJournalsExactFrameBeforeReturn(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	raw := "WT=28460;ST=1;OVERLOAD=0;FAULT=\r\n"
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte(raw))
	}()

	j := &memoryJournal{}
	r := Reader{
		Addr: ln.Addr().String(), Timeout: time.Second, StationID: "S01", Journal: j,
		TransactionID: func() string { return "TX-001" },
	}
	reading, err := r.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reading.WeightKG != 28460 || !reading.Stable {
		t.Fatalf("unexpected reading: %+v", reading)
	}
	if len(j.events) != 1 {
		t.Fatalf("got %d journal events, want 1", len(j.events))
	}
	e := j.events[0]
	if e.TransactionID != "TX-001" || !e.ParseOK || e.WeightKG == nil || *e.WeightKG != 28460 {
		t.Fatalf("unexpected event: %+v", e)
	}
	if e.RawBase64 != base64.StdEncoding.EncodeToString([]byte(raw)) {
		t.Fatalf("raw bytes changed: got %q", e.RawBase64)
	}
}
