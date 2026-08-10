package cli

import (
	"errors"
	"testing"

	clia2a "github.com/kagent-dev/kagent/go/core/cli/internal/a2a"
	"github.com/stretchr/testify/require"
)

func TestStreamA2AEventsReturnsTerminalError(t *testing.T) {
	wantErr := errors.New("stream disconnected")
	ch := make(chan clia2a.StreamResult, 1)
	ch <- clia2a.StreamResult{Err: wantErr}
	close(ch)

	err := StreamA2AEvents(ch, false)

	require.ErrorIs(t, err, wantErr)
}
