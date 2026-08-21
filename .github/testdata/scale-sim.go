package main

import (
	"log"
	"net"
	"time"
)

func main() {
	ln, err := net.Listen("tcp", "127.0.0.1:19001")
	if err != nil { log.Fatal(err) }
	defer ln.Close()
	log.Printf("two-pass scale simulator listening on %s", ln.Addr())

	conn, err := ln.Accept()
	if err != nil { log.Fatal(err) }
	defer conn.Close()

	write := func(frame string) {
		if _, err := conn.Write([]byte(frame)); err != nil { log.Fatal(err) }
	}
	settle := func(frames ...string) {
		for _, frame := range frames { write(frame); time.Sleep(200*time.Millisecond) }
	}

	// Empty/stable proof before PASS #1 A->B.
	write("WT=0;ST=1;OVERLOAD=0;FAULT=\r\n")

	// PASS #1 gross. CI has time to establish identity and position first.
	time.Sleep(5*time.Second)
	settle(
		"WT=28300;ST=0;OVERLOAD=0;FAULT=\r\n",
		"WT=28455;ST=1;OVERLOAD=0;FAULT=\r\n",
		"WT=28460;ST=1;OVERLOAD=0;FAULT=\r\n",
	)

	// Truck has left for unloading; prove the deck empty again before the
	// explicitly CALLED B->A return is permitted to enter.
	time.Sleep(4*time.Second)
	write("WT=0;ST=1;OVERLOAD=0;FAULT=\r\n")

	// PASS #2 tare.
	time.Sleep(5*time.Second)
	settle(
		"WT=11900;ST=0;OVERLOAD=0;FAULT=\r\n",
		"WT=11825;ST=1;OVERLOAD=0;FAULT=\r\n",
		"WT=11820;ST=1;OVERLOAD=0;FAULT=\r\n",
	)

	// Keep connection alive for audit/status assertions.
	time.Sleep(30*time.Second)
}
