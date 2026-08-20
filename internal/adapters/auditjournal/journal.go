package auditjournal

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
	Seq      uint64            `json:"seq"`
	PrevHash string            `json:"prev_hash,omitempty"`
	Event    domain.AuditEvent `json:"event"`
	Hash     string            `json:"hash"`
}

// rawRecord is the persistence representation. Event is retained as exact JSON
// bytes so integrity verification never depends on decoding map[string]any and
// re-encoding values with possibly different Go dynamic types.
type rawRecord struct {
	Seq      uint64          `json:"seq"`
	PrevHash string          `json:"prev_hash,omitempty"`
	Event    json.RawMessage `json:"event"`
	Hash     string          `json:"hash"`
}

type rawHashPayload struct {
	Seq      uint64          `json:"seq"`
	PrevHash string          `json:"prev_hash,omitempty"`
	Event    json.RawMessage `json:"event"`
}

type Journal struct {
	Path string

	mu          sync.Mutex
	initialized bool
	seq         uint64
	prevHash    string
}

func (j *Journal) Append(ctx context.Context, event domain.AuditEvent) (domain.AuditRef, error) {
	if err := ctx.Err(); err != nil { return domain.AuditRef{}, err }
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.Path == "" { return domain.AuditRef{}, errors.New("audit journal path is empty") }
	dir := filepath.Dir(j.Path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil { return domain.AuditRef{}, err }
	}
	if !j.initialized {
		if err := j.loadTail(); err != nil { return domain.AuditRef{}, err }
	}

	eventJSON, err := json.Marshal(event)
	if err != nil { return domain.AuditRef{}, err }
	next := j.seq + 1
	canonical, err := json.Marshal(rawHashPayload{Seq: next, PrevHash: j.prevHash, Event: eventJSON})
	if err != nil { return domain.AuditRef{}, err }
	sum := sha256.Sum256(canonical)
	hash := hex.EncodeToString(sum[:])
	line, err := json.Marshal(rawRecord{Seq: next, PrevHash: j.prevHash, Event: eventJSON, Hash: hash})
	if err != nil { return domain.AuditRef{}, err }

	f, err := os.OpenFile(j.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil { return domain.AuditRef{}, err }
	if _, err := f.Write(append(line, '\n')); err != nil { _ = f.Close(); return domain.AuditRef{}, err }
	if err := f.Sync(); err != nil { _ = f.Close(); return domain.AuditRef{}, err }
	if err := f.Close(); err != nil { return domain.AuditRef{}, err }

	j.seq = next
	j.prevHash = hash
	return domain.AuditRef{Seq: next, Hash: hash}, nil
}

func (j *Journal) Tail(limit int) ([]Record, error) {
	if limit <= 0 { limit = 200 }
	if limit > 10000 { limit = 10000 }
	if j.Path == "" { return nil, errors.New("audit journal path is empty") }

	j.mu.Lock()
	defer j.mu.Unlock()
	f, err := os.Open(j.Path)
	if errors.Is(err, os.ErrNotExist) { return []Record{}, nil }
	if err != nil { return nil, err }
	defer f.Close()

	ring := make([]Record, limit)
	count := 0
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for s.Scan() {
		if len(s.Bytes()) == 0 { continue }
		var raw rawRecord
		if err := json.Unmarshal(s.Bytes(), &raw); err != nil { return nil, err }
		var event domain.AuditEvent
		if err := json.Unmarshal(raw.Event, &event); err != nil { return nil, err }
		ring[count%limit] = Record{Seq: raw.Seq, PrevHash: raw.PrevHash, Event: event, Hash: raw.Hash}
		count++
	}
	if err := s.Err(); err != nil { return nil, err }
	if count == 0 { return []Record{}, nil }
	n := count
	if n > limit { n = limit }
	out := make([]Record, 0, n)
	for i := count - n; i < count; i++ { out = append(out, ring[i%limit]) }
	return out, nil
}

func (j *Journal) Verify() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return Verify(j.Path)
}

func (j *Journal) loadTail() error {
	j.initialized = true
	f, err := os.Open(j.Path)
	if errors.Is(err, os.ErrNotExist) { return nil }
	if err != nil { return err }
	defer f.Close()

	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var last rawRecord
	found := false
	for s.Scan() {
		if len(s.Bytes()) == 0 { continue }
		if err := json.Unmarshal(s.Bytes(), &last); err != nil { return err }
		found = true
	}
	if err := s.Err(); err != nil { return err }
	if found { j.seq, j.prevHash = last.Seq, last.Hash }
	return nil
}

func Verify(path string) error {
	f, err := os.Open(path)
	if err != nil { return err }
	defer f.Close()
	var expected uint64 = 1
	prev := ""
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for s.Scan() {
		if len(s.Bytes()) == 0 { continue }
		var r rawRecord
		if err := json.Unmarshal(s.Bytes(), &r); err != nil { return err }
		if r.Seq != expected { return errors.New("audit journal sequence break") }
		if r.PrevHash != prev { return errors.New("audit journal previous hash mismatch") }
		canonical, err := json.Marshal(rawHashPayload{Seq: r.Seq, PrevHash: r.PrevHash, Event: r.Event})
		if err != nil { return err }
		sum := sha256.Sum256(canonical)
		if r.Hash != hex.EncodeToString(sum[:]) { return errors.New("audit journal record hash mismatch") }
		prev = r.Hash
		expected++
	}
	return s.Err()
}
