package scaleascii

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
)

type failingJournal struct{}

func (failingJournal) Append(context.Context, domain.RawWeightEvent) error {
	return errors.New("disk full")
}

func TestReaderRejectsReadingWhenAuditAppendFails(t *testing.T) {
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
		_, _ = conn.Write([]byte("WT=28460;ST=1;OVERLOAD=0;FAULT=\n"))
	}()

	r := Reader{Addr: ln.Addr().String(), Timeout: time.Second, Journal: failingJournal{}}
	reading, err := r.Read(context.Background())
	if err == nil {
		t.Fatal("expected audit append failure")
	}
	if reading.Health != domain.HealthFault {
		t.Fatalf("health=%s, want FAULT", reading.Health)
	}
}
