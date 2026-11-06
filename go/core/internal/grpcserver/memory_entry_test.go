package grpcserver

import (
	"context"

	"github.com/kagent-dev/kagent/go/core/internal/database"
)

func (s *memoryRPCStore) DeleteMemoryEntry(_ context.Context, agent, user, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	key := agent + "/" + user
	for i, row := range s.rows[key] {
		if row.ID == id {
			s.rows[key] = append(s.rows[key][:i], s.rows[key][i+1:]...)
			return nil
		}
	}
	return database.ErrNotFound
}
