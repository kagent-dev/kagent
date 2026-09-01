// Package continuation persists the private native conversation identity owned
// by one Harness Actor.
package continuation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/kagent-dev/kagent/go/harness/runtime/utils"
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
	data     state
}

// New loads or creates a continuation store for runtime.
func New(durableDir, runtime string, validate Validator) (*Store, error) {
	if err := utils.EnsurePrivateDir(durableDir); err != nil {
		return nil, fmt.Errorf("prepare continuation state directory: %w", err)
	}
	s := &Store{
		path: filepath.Join(durableDir, "state.json"), runtime: runtime,
		validate: validate, data: state{Version: stateVersion, Runtime: runtime},
	}
	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read continuation state: %w", err)
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, fmt.Errorf("decode continuation state: %w", err)
	}
	if s.data.Version != stateVersion || s.data.Runtime != runtime {
		return nil, fmt.Errorf("unsupported or corrupt %s continuation state", runtime)
	}
	if s.data.ID != "" {
		if err := validate(s.data.ID); err != nil {
			return nil, fmt.Errorf("invalid persisted continuation state: %w", err)
		}
	}
	return s, nil
}

// Load returns the currently bound continuation.
func (s *Store) Load() (string, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.ID, s.data.ID != "", nil
}

// Bind atomically binds the Actor to one continuation identity.
func (s *Store) Bind(id string) error {
	if err := s.validate(id); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.ID != "" && s.data.ID != id {
		return fmt.Errorf("actor is already bound to another %s continuation", s.runtime)
	}
	if s.data.ID == id {
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
	s.data = next
	return nil
}
