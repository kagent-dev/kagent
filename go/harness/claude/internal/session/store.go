package session

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go/harness/runtime/continuation"
)

type Store = continuation.Store

func New(durableDir string) (*Store, error) {
	return continuation.New(durableDir, "claude", validateSessionID)
}

func validateSessionID(nativeSessionID string) error {
	if _, err := uuid.Parse(nativeSessionID); err != nil {
		return fmt.Errorf("invalid Claude session ID: %w", err)
	}
	return nil
}
