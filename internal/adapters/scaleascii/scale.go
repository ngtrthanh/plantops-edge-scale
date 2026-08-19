package scaleascii

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
)

type RawWeightJournal interface {
	Append(context.Context, domain.RawWeightEvent) error
}

// Reader is a generic TCP ASCII scale adapter used as a transport skeleton.
// Replace ParseLine when the exact controller protocol is known.
// Example supported line: WT=28460;ST=1;OVERLOAD=0;FAULT=
//
// When Journal is configured every received frame is durably journaled before
// the parsed reading is returned to business logic. A journal write failure is
// therefore a read failure: an unaudited weight must never become ticket truth.
type Reader struct {
	Addr          string
	Timeout       time.Duration
	StationID     string
	TransactionID func() string
	Journal       RawWeightJournal
}

func (r Reader) Read(ctx context.Context) (domain.ScaleReading, error) {
	d := net.Dialer{Timeout: r.timeout()}
	conn, err := d.DialContext(ctx, "tcp", r.Addr)
	if err != nil {
		observed := time.Now().UTC()
		reading := domain.ScaleReading{Health: domain.HealthDisconnected, Observed: observed}
		if jerr := r.append(ctx, domain.RawWeightEvent{
			StationID: r.StationID, TransactionID: r.txID(), Kind: domain.RawWeightTransportError,
			ReceivedAtUTC: observed, Source: r.Addr, Health: domain.HealthDisconnected,
			ParseOK: false, Error: err.Error(),
		}); jerr != nil {
			return reading, fmt.Errorf("scale connect: %v; raw weight audit append: %w", err, jerr)
		}
		return reading, err
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(r.timeout()))

	raw, readErr := bufio.NewReader(conn).ReadString('\n')
	received := time.Now().UTC()
	if readErr != nil {
		reading := domain.ScaleReading{Health: domain.HealthDisconnected, Observed: received}
		event := domain.RawWeightEvent{
			StationID: r.StationID, TransactionID: r.txID(), Kind: domain.RawWeightTransportError,
			ReceivedAtUTC: received, Source: r.Addr, RawBase64: base64.StdEncoding.EncodeToString([]byte(raw)),
			RawText: raw, Health: domain.HealthDisconnected, ParseOK: false, Error: readErr.Error(),
		}
		if jerr := r.append(ctx, event); jerr != nil {
			return reading, fmt.Errorf("scale read: %v; raw weight audit append: %w", readErr, jerr)
		}
		return reading, readErr
	}

	reading, parseErr := ParseLine(raw)
	reading.Observed = received
	event := domain.RawWeightEvent{
		StationID: r.StationID, TransactionID: r.txID(), Kind: domain.RawWeightFrame,
		ReceivedAtUTC: received, Source: r.Addr, RawBase64: base64.StdEncoding.EncodeToString([]byte(raw)),
		RawText: raw, Health: reading.Health, ParseOK: parseErr == nil,
	}
	if parseErr != nil {
		event.Health = domain.HealthFault
		event.Error = parseErr.Error()
	} else {
		weight, stable, overload := reading.WeightKG, reading.Stable, reading.Overload
		event.WeightKG, event.Stable, event.Overload = &weight, &stable, &overload
		event.Fault = reading.Fault
	}
	if jerr := r.append(ctx, event); jerr != nil {
		return domain.ScaleReading{Health: domain.HealthFault, Observed: received}, fmt.Errorf("raw weight audit append: %w", jerr)
	}
	if parseErr != nil {
		return domain.ScaleReading{Health: domain.HealthFault, Observed: received}, parseErr
	}
	return reading, nil
}

func (r Reader) append(ctx context.Context, event domain.RawWeightEvent) error {
	if r.Journal == nil {
		return nil
	}
	return r.Journal.Append(ctx, event)
}

func (r Reader) txID() string {
	if r.TransactionID == nil {
		return ""
	}
	return r.TransactionID()
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
