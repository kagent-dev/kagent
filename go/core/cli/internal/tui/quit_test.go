package tui

import (
	"context"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	clia2a "github.com/kagent-dev/kagent/go/core/cli/internal/a2a"
)

func openWorkspace(t *testing.T) *workspaceModel {
	t.Helper()
	session := readySession("44444444-4444-4444-4444-444444444444", "reporter")
	m := testWorkspace(t, &fakeLister{pages: []*apiv1.ListSessionsResponse{page("", session)}})
	loaded(m)
	m.resize()
	m.selectSession(session)
	require.NotNil(t, m.chat)
	return m
}

func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, quit := cmd().(tea.QuitMsg)
	return quit
}

// During a turn the first ctrl+c cancels it; a second soon after exits, and esc never does.
func TestCtrlCCancelsATurnThenExits(t *testing.T) {
	m := openWorkspace(t)
	streamCtx := make(chan context.Context, 1)
	m.chat.send = func(ctx context.Context, _ *a2atype.SendMessageRequest) <-chan clia2a.StreamResult {
		streamCtx <- ctx
		return make(chan clia2a.StreamResult)
	}
	m.chat.submit("go")
	ctx := <-streamCtx

	_, handled := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	assert.False(t, handled, "esc does not quit")
	assert.NoError(t, ctx.Err())

	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	require.Error(t, ctx.Err(), "the turn is cancelled")
	assert.Contains(t, ansi.Strip(m.View()), "Press ctrl+c again to exit")
	m.Update(streamMsg{ch: m.chat.streamCh, StreamResult: clia2a.StreamResult{Err: ctx.Err()}})
	got := ansi.Strip(transcript(m.chat))
	assert.Contains(t, got, "⊘ Cancelled")
	assert.NotContains(t, got, "Connection error")

	m.quitArmed = time.Now().Add(-noticeTTL)
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.WithinDuration(t, time.Now(), m.quitArmed, time.Second, "too late to count, so it arms again")
	cmd, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.True(t, quits(cmd))
}

// The exit warning counts the queued messages it would drop.
func TestExitWarnsOfQueuedMessages(t *testing.T) {
	m := openWorkspace(t)
	m.chat.queue = []string{"a", "b"}
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.Contains(t, ansi.Strip(m.View()), "Press ctrl+c again to exit · 2 queued messages will be dropped")
	assert.Len(t, m.chat.queue, 2, "the first ctrl+c keeps the queue")
}

// A cancelled turn pauses the queue, and up pulls it back as usual.
func TestQueuePausesOnCancel(t *testing.T) {
	m, sent, _ := queueChat(t)
	enter(m, "go")
	enter(m, "after cancel")
	m.cancelTurn()
	m.Update(streamMsg{ch: m.streamCh, StreamResult: clia2a.StreamResult{Err: context.Canceled}})
	assert.Len(t, *sent, 1, "a cancelled turn pauses the queue")
	assert.Contains(t, ansi.Strip(m.View()), "paused after a cancelled turn")
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, "after cancel", m.input.Value())
	assert.Empty(t, m.queuePaused)
}
