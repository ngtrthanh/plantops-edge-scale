package main

import (
	"encoding/binary"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

type state struct {
	mu sync.RWMutex
	di [32]bool
	coils [32]bool
}

func main() {
	s := &state{}
	// Initial physical state: empty lane, barriers closed, safety circuit clear.
	s.di[5] = true
	s.di[7] = true
	s.di[8] = true

	ln, err := net.Listen("tcp", "127.0.0.1:19002")
	if err != nil { log.Fatal(err) }
	defer ln.Close()
	log.Printf("Modbus I/O simulator listening on %s", ln.Addr())

	go simulateTruck(s)
	for {
		conn, err := ln.Accept()
		if err != nil { log.Fatal(err) }
		go handle(conn, s)
	}
}

func simulateTruck(s *state) {
	time.Sleep(2 * time.Second)
	setDI(s, 0, true) // ENTRY_PRESENT
	log.Printf("truck: entry present")

	waitFor(15*time.Second, func() bool { return coil(s,5) && coil(s,1) })
	time.Sleep(400*time.Millisecond)
	setDI(s,0,false)
	setDI(s,1,true)
	setDI(s,2,true)
	log.Printf("truck: fully positioned on deck")

	waitFor(20*time.Second, func() bool { return coil(s,6) && coil(s,3) })
	time.Sleep(400*time.Millisecond)
	setDI(s,1,false)
	setDI(s,2,false)
	setDI(s,3,true)
	log.Printf("truck: exit sensor active")
	time.Sleep(700*time.Millisecond)
	setDI(s,3,false)
	log.Printf("truck: clear of exit")
}

func waitFor(timeout time.Duration, fn func() bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() { return }
		time.Sleep(50*time.Millisecond)
	}
	log.Printf("simulator wait condition timed out")
}

func handle(conn net.Conn, s *state) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3*time.Second))
	header := make([]byte,7)
	if _,err:=io.ReadFull(conn,header); err!=nil { return }
	length:=binary.BigEndian.Uint16(header[4:6])
	if length<2 || length>260 { return }
	pdu:=make([]byte,int(length)-1)
	if _,err:=io.ReadFull(conn,pdu); err!=nil { return }
	resp:=process(pdu,s)
	out:=make([]byte,7+len(resp))
	copy(out[:2],header[:2])
	binary.BigEndian.PutUint16(out[2:4],0)
	binary.BigEndian.PutUint16(out[4:6],uint16(len(resp)+1))
	out[6]=header[6]
	copy(out[7:],resp)
	_,_=conn.Write(out)
}

func process(pdu []byte, s *state) []byte {
	if len(pdu)<1 { return []byte{0x80,0x03} }
	switch pdu[0] {
	case 0x01,0x02:
		if len(pdu)!=5 { return []byte{pdu[0]|0x80,0x03} }
		start:=binary.BigEndian.Uint16(pdu[1:3])
		qty:=binary.BigEndian.Uint16(pdu[3:5])
		if qty==0 || int(start)+int(qty)>32 { return []byte{pdu[0]|0x80,0x02} }
		byteCount:=int((qty+7)/8)
		resp:=make([]byte,2+byteCount)
		resp[0]=pdu[0]; resp[1]=byte(byteCount)
		s.mu.RLock()
		for i:=0;i<int(qty);i++ {
			var v bool
			if pdu[0]==0x02 { v=s.di[int(start)+i] } else { v=s.coils[int(start)+i] }
			if v { resp[2+i/8]|=1<<uint(i%8) }
		}
		s.mu.RUnlock()
		return resp
	case 0x05:
		if len(pdu)!=5 { return []byte{0x85,0x03} }
		addr:=binary.BigEndian.Uint16(pdu[1:3])
		if addr>=32 { return []byte{0x85,0x02} }
		value:=binary.BigEndian.Uint16(pdu[3:5])
		on:=value==0xFF00
		if value!=0xFF00 && value!=0x0000 { return []byte{0x85,0x03} }
		s.mu.Lock()
		s.coils[addr]=on
		// Barrier controller feedback: command is an OPEN request. Dropping it
		// lets the simulated barrier controller close locally.
		if addr==5 {
			s.di[4]=on; s.di[5]=!on
		}
		if addr==6 {
			s.di[6]=on; s.di[7]=!on
		}
		s.mu.Unlock()
		return append([]byte(nil),pdu...)
	default:
		return []byte{pdu[0]|0x80,0x01}
	}
}

func setDI(s *state, addr int, v bool) { s.mu.Lock(); s.di[addr]=v; s.mu.Unlock() }
func coil(s *state, addr int) bool { s.mu.RLock(); defer s.mu.RUnlock(); return s.coils[addr] }
