package scaleascii

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
)

// Reader is a generic TCP ASCII scale adapter used as a transport skeleton.
// Replace ParseLine when the exact controller protocol is known.
// Example supported line: WT=28460;ST=1;OVERLOAD=0;FAULT=
type Reader struct {
	Addr    string
	Timeout time.Duration
}

func (r Reader) Read(ctx context.Context) (domain.ScaleReading, error) {
	d := net.Dialer{Timeout: r.timeout()}
	conn, err := d.DialContext(ctx, "tcp", r.Addr)
	if err != nil {
		return domain.ScaleReading{Health: domain.HealthDisconnected, Observed: time.Now().UTC()}, err
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(r.timeout()))

	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return domain.ScaleReading{Health: domain.HealthDisconnected, Observed: time.Now().UTC()}, err
	}
	return ParseLine(line)
}

func (r Reader) timeout() time.Duration {
	if r.Timeout <= 0 {
		return 2 * time.Second
	}
	return r.Timeout
}

func ParseLine(line string) (domain.ScaleReading, error) {
	out := domain.ScaleReading{Health: domain.HealthHealthy, Observed: time.Now().UTC()}
	seenWeight := false
	seenStable := false

	for _, token := range strings.Split(strings.TrimSpace(line), ";") {
		kv := strings.SplitN(strings.TrimSpace(token), "=", 2)
		if len(kv) != 2 {
			continue
		}
		key, val := strings.ToUpper(strings.TrimSpace(kv[0])), strings.TrimSpace(kv[1])
		switch key {
		case "WT", "WEIGHT":
			w, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return domain.ScaleReading{}, fmt.Errorf("parse weight: %w", err)
			}
			out.WeightKG, seenWeight = w, true
		case "ST", "STABLE":
			out.Stable = val == "1" || strings.EqualFold(val, "true") || strings.EqualFold(val, "stable")
			seenStable = true
		case "OVERLOAD":
			out.Overload = val == "1" || strings.EqualFold(val, "true")
		case "FAULT":
			out.Fault = val
		}
	}

	if !seenWeight || !seenStable {
		return domain.ScaleReading{}, errors.New("scale frame missing authoritative weight or stable field")
	}
	if out.Fault != "" {
		out.Health = domain.HealthFault
	}
	return out, nil
}
