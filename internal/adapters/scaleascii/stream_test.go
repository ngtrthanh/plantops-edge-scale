package scaleascii

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
)

type streamMemoryJournal struct {
	mu     sync.Mutex
	events []domain.RawWeightEvent
}

func (m *streamMemoryJournal) Append(_ context.Context, e domain.RawWeightEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
	return nil
}

func (m *streamMemoryJournal) snapshot() []domain.RawWeightEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]domain.RawWeightEvent, len(m.events))
	copy(out, m.events)
	return out
}

func TestStreamCollectorAuditsEveryFrameBeforePublishing(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte(
			"WT=1200;ST=0;OVERLOAD=0;FAULT=\r\n" +
				"WT=28420;ST=0;OVERLOAD=0;FAULT=\r\n" +
				"WT=28460;ST=1;OVERLOAD=0;FAULT=\r\n",
		))
		<-time.After(200 * time.Millisecond)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	j := &streamMemoryJournal{}
	var mu sync.Mutex
	published := make([]domain.ScaleReading, 0, 3)

	collector := &StreamCollector{
		Addr: ln.Addr().String(), StationID: "S01", Journal: j,
		ReconnectDelay: time.Second,
		OnReading: func(r domain.ScaleReading) {
			mu.Lock()
			published = append(published, r)
			count := len(published)
			mu.Unlock()
			if count == 3 {
				cancel()
			}
		},
	}

	if err := collector.Run(ctx); err != nil {
		t.Fatal(err)
	}

	events := j.snapshot()
	frames := make([]domain.RawWeightEvent, 0, 3)
	for _, e := range events {
		if e.Kind == domain.RawWeightFrame {
			frames = append(frames, e)
		}
	}
	if len(frames) != 3 {
		t.Fatalf("got %d raw frames, want 3; all events=%+v", len(frames), events)
	}
	wantWeights := []int64{1200, 28420, 28460}
	for i, want := range wantWeights {
		if frames[i].WeightKG == nil || *frames[i].WeightKG != want {
			t.Fatalf("frame %d weight=%v want=%d", i, frames[i].WeightKG, want)
		}
		if frames[i].ReceivedAtUTC.IsZero() || frames[i].RawBase64 == "" {
			t.Fatalf("frame %d missing audit timestamp/raw bytes: %+v", i, frames[i])
		}
	}
	if frames[2].Stable == nil || !*frames[2].Stable {
		t.Fatalf("final frame must preserve stable=true: %+v", frames[2])
	}
}
