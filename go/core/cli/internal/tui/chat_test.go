package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	clia2a "github.com/kagent-dev/kagent/go/core/cli/internal/a2a"
	"github.com/stretchr/testify/require"
)

func TestChatModelDisplaysStreamError(t *testing.T) {
	send := func(context.Context, *a2atype.SendMessageRequest) <-chan clia2a.StreamResult {
		ch := make(chan clia2a.StreamResult)
		close(ch)
		return ch
	}
	model := newChatModel("default/agent", "session-1", send, false)
	model.streaming = true
	model.working = true

	updated, cmd := model.Update(clia2a.StreamResult{Err: errors.New("stream disconnected")})
	got := updated.(*chatModel)

	require.Nil(t, cmd)
	require.False(t, got.streaming)
	require.False(t, got.working)
	require.True(t, strings.Contains(got.history, "Error: stream disconnected"))
}
