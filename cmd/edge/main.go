package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/ingress"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/rawjournal"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/adapters/scaleascii"
	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/httpapi"
)

var (
	version = "dev"
	gitSHA  = "dev"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "HTTP listen address")
	stationID := flag.String("station-id", "EDGE-01", "stable station identifier used in audit records")
	scaleAddr := flag.String("scale-addr", "", "scale controller TCP address host:port; empty disables live collector")
	rawWeightJournal := flag.String("raw-weight-journal", "data/raw-weight.jsonl", "append-only raw weight audit journal path")
	reconnectDelay := flag.Duration("scale-reconnect-delay", 2*time.Second, "delay before reconnecting to scale controller")
	flag.Parse()

	audit := &rawjournal.Journal{Path: *rawWeightJournal}
	if err := audit.Verify(); err != nil && !os.IsNotExist(err) {
		log.Fatalf("raw weight audit integrity check failed: %v", err)
	}

	scaleMonitor := scaleascii.NewMonitor(*scaleAddr != "", *scaleAddr)
	if *scaleAddr != "" {
		collector := &scaleascii.StreamCollector{
			Addr: *scaleAddr,
			StationID: *stationID,
			Journal: audit,
			ReconnectDelay: *reconnectDelay,
			OnReading: scaleMonitor.Reading,
			OnFault: scaleMonitor.Fault,
		}
		go func() {
			if err := collector.Run(context.Background()); err != nil {
				scaleMonitor.Fault(err)
				// Keep HTTP/operator visibility alive, but stop consuming weights.
				// The future state engine treats this condition as FAULT_LOCKOUT.
				log.Printf("CRITICAL raw weight collector stopped: %v", err)
			}
		}()
		log.Printf("raw weight collector enabled station=%s scale=%s journal=%s", *stationID, *scaleAddr, *rawWeightJournal)
	} else {
		log.Printf("raw weight collector disabled: start with -scale-addr host:port; existing audit remains readable at %s", *rawWeightJournal)
	}

	s := &httpapi.Server{
		RFID: &ingress.RFID{}, LPR: &ingress.LPR{},
		WeightAudit: audit, ScaleMonitor: scaleMonitor,
		Version: version, GitSHA: gitSHA,
	}
	httpServer := &http.Server{
		Addr: *listen, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("plantops-edge-scale Go vNext %s sha=%s listening on http://%s", version, gitSHA, *listen)
	log.Fatal(httpServer.ListenAndServe())
}
