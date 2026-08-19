package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/ingress"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/httpapi"
)

var (
	version = "dev"
	gitSHA  = "dev"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "HTTP listen address")
	flag.Parse()

	s := &httpapi.Server{
		RFID: &ingress.RFID{}, LPR: &ingress.LPR{}, Version: version, GitSHA: gitSHA,
	}
	httpServer := &http.Server{
		Addr: *listen, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("plantops-edge-scale Go vNext %s sha=%s listening on http://%s", version, gitSHA, *listen)
	log.Fatal(httpServer.ListenAndServe())
}
