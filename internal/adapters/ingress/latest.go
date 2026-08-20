package ingress

import (
	"sync"
	"time"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
)

type RFID struct {
	mu            sync.RWMutex
	v             domain.RFIDObservation
	OnObservation func(domain.RFIDObservation)
}

func (r *RFID) Ingest(tag string, quality float64) domain.RFIDObservation {
	obs := domain.RFIDObservation{Tag: tag, Quality: quality, Health: domain.HealthHealthy, Observed: time.Now().UTC()}
	r.mu.Lock()
	r.v = obs
	callback := r.OnObservation
	r.mu.Unlock()
	if callback != nil {
		callback(obs)
	}
	return obs
}

func (r *RFID) Latest() domain.RFIDObservation {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.v
}

type LPR struct {
	mu            sync.RWMutex
	v             domain.LPRObservation
	OnObservation func(domain.LPRObservation)
}

func (l *LPR) Ingest(plate string, confidence float64, imageRef string) domain.LPRObservation {
	obs := domain.LPRObservation{Plate: plate, Confidence: confidence, ImageRef: imageRef, Health: domain.HealthHealthy, Observed: time.Now().UTC()}
	l.mu.Lock()
	l.v = obs
	callback := l.OnObservation
	l.mu.Unlock()
	if callback != nil {
		callback(obs)
	}
	return obs
}

func (l *LPR) Latest() domain.LPRObservation {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.v
}
