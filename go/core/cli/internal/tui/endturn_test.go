package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kagenta2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	clia2a "github.com/kagent-dev/kagent/go/core/cli/internal/a2a"
)

// lastLine is the transcript's closing line.
func lastLine(m *chatModel) string {
	return ansi.Strip(m.blocks[len(m.blocks)-1].text)
}

func TestTurnEndLines(t *testing.T) {
	t.Run("done says how long, how many tools and how many tokens", func(t *testing.T) {
		m := newTestChatModel()
		m.setWorkingTime(time.Time{})
		m.workStart = time.Now().Add(-12 * time.Second)
		for _, name := range []string{"read_file", "grep_file", "bash"} {
			m.appendEvent(a2atype.NewArtifactEvent(reqCtx(), dataPart("function_call", name, map[string]any{"args": map[string]any{}})))
		}
		answer := a2atype.NewArtifactEvent(reqCtx(), a2atype.NewTextPart("All good."))
		answer.Artifact.Metadata = map[string]any{kagenta2a.UsageMetadataKey: map[string]any{"promptTokenCount": float64(1100), "candidatesTokenCount": float64(300)}}
		m.appendEvent(answer)
		m.appendEvent(a2atype.NewStatusUpdateEvent(reqCtx(), a2atype.TaskStateCompleted, nil))

		assert.Equal(t, "✓ Done in 12s · 3 tools · 1.4k tokens", lastLine(m))
		assert.Empty(t, m.inlineStatus(), "the closing line replaces the status line")
		assert.False(t, m.working)
		assert.True(t, m.attention)
	})
	t.Run("failed says when and why", func(t *testing.T) {
		m := newTestChatModel()
		m.setWorkingTime(time.Time{})
		m.workStart = time.Now().Add(-8 * time.Second)
		reason := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("model quota exceeded\nretry later"))
		m.appendEvent(a2atype.NewStatusUpdateEvent(reqCtx(), a2atype.TaskStateFailed, reason))
		assert.Equal(t, "✗ Failed after 8s · model quota exceeded", lastLine(m))
	})
	t.Run("cancelled", func(t *testing.T) {
		m := newTestChatModel()
		m.setWorkingTime(time.Time{})
		m.appendEvent(a2atype.NewStatusUpdateEvent(reqCtx(), a2atype.TaskStateCanceled, nil))
		assert.Equal(t, "⊘ Cancelled", lastLine(m))
	})
}

func TestCompactCount(t *testing.T) {
	for n, want := range map[int64]string{950: "950", 1000: "1k", 1420: "1.4k", 12345: "12k"} {
		assert.Equal(t, want, compactCount(n), "%d", n)
	}
}

// The bell rings only when a turn ends, or waits on the user, while the terminal reports it is unfocused.
func TestBellWhenAway(t *testing.T) {
	rings := 0
	previous := ringBell
	ringBell = func() { rings++ }
	t.Cleanup(func() { ringBell = previous })

	session := readySession("44444444-4444-4444-4444-444444444444", "reporter")
	m := testWorkspace(t, &fakeLister{pages: []*apiv1.ListSessionsResponse{page("", session)}})
	loaded(m)
	m.resize()
	m.selectSession(session)
	require.NotNil(t, m.chat)
	finish := func() {
		m.chat.send = func(context.Context, *a2atype.SendMessageRequest) <-chan clia2a.StreamResult {
			ch := make(chan clia2a.StreamResult)
			close(ch)
			return ch
		}
		m.chat.submit("go")
		m.Update(streamMsg{ch: m.chat.streamCh, StreamResult: clia2a.StreamResult{Event: a2atype.NewStatusUpdateEvent(reqCtx(), a2atype.TaskStateCompleted, nil)}})
	}

	finish()
	assert.Zero(t, rings, "focused, or a terminal that never reports focus")

	m.Update(tea.BlurMsg{})
	finish()
	assert.Equal(t, 1, rings, "unfocused when the turn finished")

	m.chat.submit("again")
	m.Update(streamMsg{ch: m.chat.streamCh, StreamResult: clia2a.StreamResult{Event: a2atype.NewStatusUpdateEvent(reqCtx(), a2atype.TaskStateInputRequired, nil)}})
	assert.Equal(t, 2, rings, "unfocused when the turn began waiting for input")

	m.cfg.MuteBell = true
	finish()
	assert.Equal(t, 2, rings, "turned off")

	m.cfg.MuteBell = false
	m.Update(tea.FocusMsg{})
	finish()
	assert.Equal(t, 2, rings, "focused again")
}

// While a turn runs, its status is the transcript's last line and scrolls with it.
func TestInlineTurnStatus(t *testing.T) {
	m := newTestChatModel()
	m.vp.Width, m.vp.Height = 82, 20
	m.appendUser("go")
	m.setWorkingTime(time.Time{})
	m.appendEvent(a2atype.NewArtifactEvent(reqCtx(), a2atype.NewTextPart("Partial answer")))
	rows := strings.Split(strings.TrimRight(ansi.Strip(m.vp.View()), " \n"), "\n")
	assert.Contains(t, rows[len(rows)-1], "Answering… 0s", "the status follows the newest content")
	assert.Contains(t, rows[len(rows)-3], "Partial answer")
	assert.NotContains(t, ansi.Strip(m.View()), "Answering… 0s\n────", "no fixed status row above the divider")

	m.appendEvent(a2atype.NewStatusUpdateEvent(reqCtx(), a2atype.TaskStateCompleted, nil))
	rows = strings.Split(strings.TrimRight(ansi.Strip(m.vp.View()), " \n"), "\n")
	assert.Contains(t, rows[len(rows)-1], "✓ Done in")
	assert.NotContains(t, ansi.Strip(m.View()), "Answering")
}
