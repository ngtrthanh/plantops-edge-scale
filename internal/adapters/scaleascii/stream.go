package scaleascii

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
)

// StreamCollector keeps one scale connection open and consumes every newline-
// delimited frame continuously. It is the production-shaped counterpart to the
// one-shot Reader transport skeleton.
//
// Audit ordering is deliberate:
//
//   controller bytes -> durable raw journal -> parsed reading callback
//
// If the raw journal cannot be written, Run returns an error. An unaudited
// weight observation must never be allowed to become business/ticket truth.
type StreamCollector struct {
	Addr           string
	StationID      string
	TransactionID  func() string
	Journal        RawWeightJournal
	ReconnectDelay time.Duration
	DialTimeout    time.Duration
	OnReading      func(domain.ScaleReading)
	OnFault        func(error)
}

func (c *StreamCollector) Run(ctx context.Context) error {
	if c.Addr == "" {
		return fmt.Errorf("scale stream address is empty")
	}
	if c.Journal == nil {
		return fmt.Errorf("raw weight journal is required")
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}

		d := net.Dialer{Timeout: c.dialTimeout()}
		conn, err := d.DialContext(ctx, "tcp", c.Addr)
		if err != nil {
			observed := time.Now().UTC()
			if jerr := c.Journal.Append(ctx, domain.RawWeightEvent{
				StationID: c.StationID, TransactionID: c.txID(),
				Kind: domain.RawWeightTransportError, ReceivedAtUTC: observed,
				Source: c.Addr, Health: domain.HealthDisconnected,
				ParseOK: false, Error: err.Error(),
			}); jerr != nil {
				return fmt.Errorf("scale connect: %v; raw weight audit append: %w", err, jerr)
			}
			c.fault(err)
			if !sleepContext(ctx, c.reconnectDelay()) {
				return nil
			}
			continue
		}

		err = c.consume(ctx, conn)
		_ = conn.Close()
		if err != nil {
			return err
		}
		if !sleepContext(ctx, c.reconnectDelay()) {
			return nil
		}
	}
}

func (c *StreamCollector) consume(ctx context.Context, conn net.Conn) error {
	reader := bufio.NewReader(conn)
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}

		raw, readErr := reader.ReadString('\n')
		received := time.Now().UTC()

		if len(raw) > 0 {
			reading, parseErr := ParseLine(raw)
			reading.Observed = received
			event := domain.RawWeightEvent{
				StationID: c.StationID, TransactionID: c.txID(),
				Kind: domain.RawWeightFrame, ReceivedAtUTC: received,
				Source: c.Addr,
				RawBase64: base64.StdEncoding.EncodeToString([]byte(raw)),
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

			if err := c.Journal.Append(ctx, event); err != nil {
				return fmt.Errorf("raw weight audit append: %w", err)
			}
			if parseErr != nil {
				c.fault(parseErr)
			} else if c.OnReading != nil {
				c.OnReading(reading)
			}
		}

		if readErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			// If bytes were returned together with the transport error they were
			// already preserved above as a FRAME. The error itself is a separate
			// audit fact so reconnect history is reconstructable.
			if err := c.Journal.Append(ctx, domain.RawWeightEvent{
				StationID: c.StationID, TransactionID: c.txID(),
				Kind: domain.RawWeightTransportError, ReceivedAtUTC: received,
				Source: c.Addr, Health: domain.HealthDisconnected,
				ParseOK: false, Error: readErr.Error(),
			}); err != nil {
				return fmt.Errorf("scale disconnect audit append: %w", err)
			}
			c.fault(readErr)
			return nil
		}
	}
}

func (c *StreamCollector) txID() string {
	if c.TransactionID == nil {
		return ""
	}
	return c.TransactionID()
}

func (c *StreamCollector) dialTimeout() time.Duration {
	if c.DialTimeout <= 0 {
		return 2 * time.Second
	}
	return c.DialTimeout
}

func (c *StreamCollector) reconnectDelay() time.Duration {
	if c.ReconnectDelay <= 0 {
		return 2 * time.Second
	}
	return c.ReconnectDelay
}

func (c *StreamCollector) fault(err error) {
	if c.OnFault != nil && err != nil {
		c.OnFault(err)
	}
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
