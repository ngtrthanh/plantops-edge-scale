package ingress

import (
	"sync"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
)

type RFID struct {
	mu sync.RWMutex
	v  domain.RFIDObservation
}

func (r *RFID) Ingest(tag string, quality float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.v = domain.RFIDObservation{Tag: tag, Quality: quality, Health: domain.HealthHealthy, Observed: time.Now().UTC()}
}

func (r *RFID) Latest() domain.RFIDObservation {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.v
}

type LPR struct {
	mu sync.RWMutex
	v  domain.LPRObservation
}

func (l *LPR) Ingest(plate string, confidence float64, imageRef string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.v = domain.LPRObservation{Plate: plate, Confidence: confidence, ImageRef: imageRef, Health: domain.HealthHealthy, Observed: time.Now().UTC()}
}

func (l *LPR) Latest() domain.LPRObservation {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.v
}
