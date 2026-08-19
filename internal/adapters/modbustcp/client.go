package modbustcp

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"time"
)

type Client struct {
	Addr    string
	UnitID  byte
	Timeout time.Duration
	tx      atomic.Uint32
}

func (c *Client) ReadDiscreteInputs(ctx context.Context, start, quantity uint16) ([]bool, error) {
	return c.readBits(ctx, 0x02, start, quantity)
}

func (c *Client) ReadCoils(ctx context.Context, start, quantity uint16) ([]bool, error) {
	return c.readBits(ctx, 0x01, start, quantity)
}

func (c *Client) WriteSingleCoil(ctx context.Context, address uint16, on bool) error {
	value := uint16(0x0000)
	if on {
		value = 0xFF00
	}
	pdu := []byte{0x05, byte(address >> 8), byte(address), byte(value >> 8), byte(value)}
	resp, err := c.exchange(ctx, pdu)
	if err != nil {
		return err
	}
	if len(resp) != 5 || resp[0] != 0x05 || binary.BigEndian.Uint16(resp[1:3]) != address || binary.BigEndian.Uint16(resp[3:5]) != value {
		return errors.New("invalid Modbus write-single-coil response")
	}
	return nil
}

func (c *Client) readBits(ctx context.Context, function byte, start, quantity uint16) ([]bool, error) {
	if quantity == 0 || quantity > 2000 {
		return nil, fmt.Errorf("invalid quantity %d", quantity)
	}
	pdu := []byte{function, byte(start >> 8), byte(start), byte(quantity >> 8), byte(quantity)}
	resp, err := c.exchange(ctx, pdu)
	if err != nil {
		return nil, err
	}
	if len(resp) < 2 || resp[0] != function {
		return nil, errors.New("invalid Modbus read response")
	}
	byteCount := int(resp[1])
	if len(resp) != 2+byteCount {
		return nil, errors.New("invalid Modbus byte count")
	}
	out := make([]bool, quantity)
	for i := 0; i < int(quantity); i++ {
		out[i] = resp[2+i/8]&(1<<uint(i%8)) != 0
	}
	return out, nil
}

func (c *Client) exchange(ctx context.Context, pdu []byte) ([]byte, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", c.Addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	txid := uint16(c.tx.Add(1))
	frame := make([]byte, 7+len(pdu))
	binary.BigEndian.PutUint16(frame[0:2], txid)
	binary.BigEndian.PutUint16(frame[2:4], 0)
	binary.BigEndian.PutUint16(frame[4:6], uint16(len(pdu)+1))
	frame[6] = c.UnitID
	copy(frame[7:], pdu)

	if _, err := conn.Write(frame); err != nil {
		return nil, err
	}
	header := make([]byte, 7)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	if binary.BigEndian.Uint16(header[0:2]) != txid || binary.BigEndian.Uint16(header[2:4]) != 0 {
		return nil, errors.New("invalid Modbus MBAP header")
	}
	length := binary.BigEndian.Uint16(header[4:6])
	if length < 2 || length > 260 {
		return nil, fmt.Errorf("invalid Modbus length %d", length)
	}
	body := make([]byte, int(length)-1)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, err
	}
	if body[0]&0x80 != 0 {
		code := byte(0)
		if len(body) > 1 {
			code = body[1]
		}
		return nil, fmt.Errorf("Modbus exception function=0x%02x code=0x%02x", body[0], code)
	}
	return body, nil
}
