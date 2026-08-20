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

	frames := []string{
		"WT=1200;ST=0;OVERLOAD=0;FAULT=\r\n",
		"WT=28420;ST=0;OVERLOAD=0;FAULT=\r\n",
		"WT=28460;ST=1;OVERLOAD=0;FAULT=\r\n",
	}
	for _, frame := range frames {
		if _, err := conn.Write([]byte(frame)); err != nil {
			log.Fatal(err)
		}
		time.Sleep(150 * time.Millisecond)
	}
	// Keep the connection alive so /api/scale/status remains connected while CI
	// verifies the audit timeline.
	time.Sleep(30 * time.Second)
}
