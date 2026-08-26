package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
)

const stateVersion = 2

type state struct {
	Version   int    `json:"version"`
	Runtime   string `json:"runtime"`
	SessionID string `json:"session_id,omitempty"`
}

type Store struct {
	mu   sync.RWMutex
	path string
	data state
}

func New(durableDir string) (*Store, error) {
	if durableDir == "" {
		return nil, fmt.Errorf("durable directory is required")
	}
	if err := os.MkdirAll(durableDir, 0o700); err != nil {
		return nil, fmt.Errorf("create session state directory: %w", err)
	}
	if err := os.Chmod(durableDir, 0o700); err != nil {
		return nil, fmt.Errorf("secure session state directory: %w", err)
	}
	s := &Store{path: filepath.Join(durableDir, "state.json")}
	s.data = state{Version: stateVersion, Runtime: "claude"}
	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read session state: %w", err)
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, fmt.Errorf("decode session state: %w", err)
	}
	if s.data.Version != stateVersion || s.data.Runtime != "claude" {
		return nil, fmt.Errorf("unsupported or corrupt Claude session state")
	}
	if s.data.SessionID != "" {
		if err := validateSessionID(s.data.SessionID); err != nil {
			return nil, fmt.Errorf("invalid persisted session state: %w", err)
		}
	}
	return s, nil
}

func (s *Store) Load() (string, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.SessionID, s.data.SessionID != "", nil
}

func (s *Store) Bind(nativeSessionID string) error {
	if err := validateSessionID(nativeSessionID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.SessionID != "" && s.data.SessionID != nativeSessionID {
		return fmt.Errorf("actor is already bound to another Claude session")
	}
	if s.data.SessionID == nativeSessionID {
		return nil
	}
	next := state{Version: stateVersion, Runtime: "claude", SessionID: nativeSessionID}
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return fmt.Errorf("encode session state: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".sessions-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary session state: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("secure temporary session state: %w", err)
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary session state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temporary session state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary session state: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace session state: %w", err)
	}
	s.data = next
	return nil
}

func validateSessionID(nativeSessionID string) error {
	if _, err := uuid.Parse(nativeSessionID); err != nil {
		return fmt.Errorf("invalid Claude session ID: %w", err)
	}
	return nil
}
