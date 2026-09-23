// Package continuation persists the private native conversation identity owned
// by one Harness Actor.
package continuation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/kagent-dev/kagent/go/harness/internal/utils"
)

const stateVersion = 2

type state struct {
	Version int    `json:"version"`
	Runtime string `json:"runtime"`
	ID      string `json:"session_id,omitempty"`
}

// Validator validates one runtime-specific opaque continuation ID.
type Validator func(string) error

// Store is an atomic, actor-local continuation store.
type Store struct {
	mu       sync.RWMutex
	path     string
	runtime  string
	validate Validator
}

// New opens or creates a continuation store for runtime.
func New(durableDir, runtime string, validate Validator) (*Store, error) {
	if err := utils.EnsurePrivateDir(durableDir); err != nil {
		return nil, fmt.Errorf("prepare continuation state directory: %w", err)
	}
	s := &Store{
		path: filepath.Join(durableDir, "state.json"), runtime: runtime,
		validate: validate,
	}
	if _, err := s.read(); err != nil {
		return nil, err
	}
	return s, nil
}

// read reloads state from the durable directory. Substrate can restore /data
// into a process resumed from a golden snapshot after New has already run, so
// process memory cannot be the source of truth for continuation identity.
func (s *Store) read() (state, error) {
	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return state{Version: stateVersion, Runtime: s.runtime}, nil
	}
	if err != nil {
		return state{}, fmt.Errorf("read continuation state: %w", err)
	}
	var persisted state
	if err := json.Unmarshal(b, &persisted); err != nil {
		return state{}, fmt.Errorf("decode continuation state: %w", err)
	}
	if persisted.Version != stateVersion || persisted.Runtime != s.runtime {
		return state{}, fmt.Errorf("unsupported or corrupt %s continuation state", s.runtime)
	}
	if persisted.ID != "" {
		if err := s.validate(persisted.ID); err != nil {
			return state{}, fmt.Errorf("invalid persisted continuation state: %w", err)
		}
	}
	return persisted, nil
}

// Load returns the currently bound continuation.
func (s *Store) Load() (string, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	persisted, err := s.read()
	if err != nil {
		return "", false, err
	}
	return persisted.ID, persisted.ID != "", nil
}

// Bind atomically binds the Actor to one continuation identity.
func (s *Store) Bind(id string) error {
	if err := s.validate(id); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	persisted, err := s.read()
	if err != nil {
		return err
	}
	if persisted.ID != "" && persisted.ID != id {
		return fmt.Errorf("actor is already bound to another %s continuation", s.runtime)
	}
	if persisted.ID == id {
		return nil
	}
	next := state{Version: stateVersion, Runtime: s.runtime, ID: id}
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return fmt.Errorf("encode continuation state: %w", err)
	}
	if err := utils.ReplacePrivateFile(s.path, b); err != nil {
		return fmt.Errorf("persist continuation state: %w", err)
	}
	return nil
}
