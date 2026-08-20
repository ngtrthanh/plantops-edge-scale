package main

import (
	"log"
	"net"
	"time"
)

func main() {
	ln, err := net.Listen("tcp", "127.0.0.1:19001")
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()
	log.Printf("scale simulator listening on %s", ln.Addr())

	conn, err := ln.Accept()
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	// First prove an empty, stable scale before entry authorization.
	if _, err := conn.Write([]byte("WT=0;ST=1;OVERLOAD=0;FAULT=\r\n")); err != nil {
		log.Fatal(err)
	}

	// Give CI enough time to inject entry/RFID/LPR/position events. Then behave
	// like a truck loading the deck and settling to a final stable weight.
	time.Sleep(5 * time.Second)
	frames := []string{
		"WT=28300;ST=0;OVERLOAD=0;FAULT=\r\n",
		"WT=28455;ST=1;OVERLOAD=0;FAULT=\r\n",
		"WT=28460;ST=1;OVERLOAD=0;FAULT=\r\n",
	}
	for _, frame := range frames {
		if _, err := conn.Write([]byte(frame)); err != nil {
			log.Fatal(err)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Keep the connection alive so workflow/audit status remains inspectable.
	time.Sleep(30 * time.Second)
}
