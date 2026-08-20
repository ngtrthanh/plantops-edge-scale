package rawjournal

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
)

type Record struct {
	Seq      uint64                `json:"seq"`
	PrevHash string                `json:"prev_hash,omitempty"`
	Event    domain.RawWeightEvent `json:"event"`
	Hash     string                `json:"hash"`
}

type hashPayload struct {
	Seq      uint64                `json:"seq"`
	PrevHash string                `json:"prev_hash,omitempty"`
	Event    domain.RawWeightEvent `json:"event"`
}

// Journal is a durable append-only JSONL bootstrap store for every raw scale
// observation. Each row chains to the previous row by SHA-256 so later audit
// can detect missing, reordered, or modified records. SQLite will replace the
// storage backend later, not the audit semantics.
type Journal struct {
	Path string

	mu          sync.Mutex
	initialized bool
	seq         uint64
	prevHash    string
}

func (j *Journal) Append(ctx context.Context, event domain.RawWeightEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.Path == "" {
		return errors.New("raw weight journal path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(j.Path), 0o755); err != nil {
		return err
	}
	if !j.initialized {
		if err := j.loadTail(); err != nil {
			return err
		}
	}

	next := j.seq + 1
	payload := hashPayload{Seq: next, PrevHash: j.prevHash, Event: event}
	canonical, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(canonical)
	hash := hex.EncodeToString(sum[:])
	record := Record{Seq: next, PrevHash: j.prevHash, Event: event, Hash: hash}
	line, err := json.Marshal(record)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(j.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	j.seq = next
	j.prevHash = hash
	return nil
}

// Tail returns the newest audit records in chronological order. It is a
// bootstrap replay helper for the operator/audit API. SQLite will later provide
// indexed time/transaction queries without changing Record semantics.
func (j *Journal) Tail(limit int) ([]Record, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 10000 {
		limit = 10000
	}
	if j.Path == "" {
		return nil, errors.New("raw weight journal path is empty")
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	f, err := os.Open(j.Path)
	if errors.Is(err, os.ErrNotExist) {
		return []Record{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	ring := make([]Record, limit)
	count := 0
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for s.Scan() {
		if len(s.Bytes()) == 0 {
			continue
		}
		var record Record
		if err := json.Unmarshal(s.Bytes(), &record); err != nil {
			return nil, err
		}
		ring[count%limit] = record
		count++
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	if count == 0 {
		return []Record{}, nil
	}

	n := count
	if n > limit {
		n = limit
	}
	out := make([]Record, 0, n)
	start := count - n
	for i := start; i < count; i++ {
		out = append(out, ring[i%limit])
	}
	return out, nil
}

func (j *Journal) Verify() error {
	return Verify(j.Path)
}

func (j *Journal) loadTail() error {
	j.initialized = true
	f, err := os.Open(j.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	buf := make([]byte, 64*1024)
	s.Buffer(buf, 4*1024*1024)
	var last Record
	found := false
	for s.Scan() {
		if len(s.Bytes()) == 0 {
			continue
		}
		if err := json.Unmarshal(s.Bytes(), &last); err != nil {
			return err
		}
		found = true
	}
	if err := s.Err(); err != nil {
		return err
	}
	if found {
		j.seq = last.Seq
		j.prevHash = last.Hash
	}
	return nil
}

func Verify(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var expectedSeq uint64 = 1
	prev := ""
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for s.Scan() {
		if len(s.Bytes()) == 0 {
			continue
		}
		var record Record
		if err := json.Unmarshal(s.Bytes(), &record); err != nil {
			return err
		}
		if record.Seq != expectedSeq {
			return errors.New("raw weight journal sequence break")
		}
		if record.PrevHash != prev {
			return errors.New("raw weight journal previous hash mismatch")
		}
		payload := hashPayload{Seq: record.Seq, PrevHash: record.PrevHash, Event: record.Event}
		canonical, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(canonical)
		if record.Hash != hex.EncodeToString(sum[:]) {
			return errors.New("raw weight journal record hash mismatch")
		}
		prev = record.Hash
		expectedSeq++
	}
	return s.Err()
}
