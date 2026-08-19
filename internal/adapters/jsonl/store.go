package jsonl

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/ngtrthanh/plantops-edge-scale/goedge/internal/domain"
)

// Store is the zero-dependency bootstrap repository. The production Go port is
// expected to replace this with SQLite behind the same TicketStore port.
type Store struct {
	Path string
	mu   sync.Mutex
}

func (s *Store) Commit(_ context.Context, ticket domain.Ticket) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(ticket)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return f.Sync()
}
