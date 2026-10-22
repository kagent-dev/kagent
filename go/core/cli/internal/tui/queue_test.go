package tui

import (
	"context"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"

	clia2a "github.com/kagent-dev/kagent/go/core/cli/internal/a2a"
)

// queueChat is a chat whose sends stay open until the test ends them, recording each message.
func queueChat(t *testing.T) (*chatModel, *[]string, func()) {
	t.Helper()
	m, _ := sizedChat(t)
	var sent []string
	var open chan clia2a.StreamResult
	m.send = func(_ context.Context, req *a2atype.SendMessageRequest) <-chan clia2a.StreamResult {
		sent = append(sent, req.Message.Parts[0].Text())
		open = make(chan clia2a.StreamResult)
		return open
	}
	end := func() {
		m.appendEvent(a2atype.NewStatusUpdateEvent(reqCtx(), a2atype.TaskStateCompleted, nil))
		close(open)
		m.Update(streamDoneMsg{ch: m.streamCh}) // a queued send happens here; its stream waits are left unrun
	}
	return m, &sent, end
}

func enter(m *chatModel, text string) {
	m.input.SetValue(text)
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
}

// Messages typed during a turn queue, show above the input, and go one per turn, in order.
func TestQueueSendsInOrderAfterEachTurn(t *testing.T) {
	m, sent, end := queueChat(t)
	enter(m, "first")
	enter(m, "second\nwith a second line")
	enter(m, "third")
	assert.Equal(t, []string{"first"}, *sent)
	view := ansi.Strip(m.View())
	assert.Contains(t, view, "⏳ Queued (2) · ↑ edit")
	assert.Contains(t, view, "  second", "each message shows its first line")
	assert.NotContains(t, view, "with a second line")

	end()
	assert.Equal(t, []string{"first", "second\nwith a second line"}, *sent, "the next goes when the turn ends")
	assert.Contains(t, ansi.Strip(m.View()), "⏳ Queued (1)")
	end()
	assert.Equal(t, []string{"first", "second\nwith a second line", "third"}, *sent)
	end()
	assert.Len(t, *sent, 3)
	assert.NotContains(t, ansi.Strip(m.View()), "Queued")
}

// Up pulls the whole queue into the input, after any draft, and empties the queue.
func TestUpPullsTheQueue(t *testing.T) {
	m, _, _ := queueChat(t)
	enter(m, "go")
	enter(m, "one")
	enter(m, "two")
	m.input.SetValue("draft")
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, "draft\none\ntwo", m.input.Value())
	assert.Empty(t, m.queue)

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, []string{"draft\none\ntwo"}, m.queue, "it queues again as one message")

	m.queue = nil
	m.input.SetValue("one\ntwo")
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 0, m.input.Line(), "with an empty queue, up moves in the input as before")
}

// A failed turn pauses the queue; Enter on an empty input resumes it.
func TestQueuePausesOnFailure(t *testing.T) {
	m, sent, _ := queueChat(t)
	enter(m, "go")
	enter(m, "next")
	m.appendEvent(a2atype.NewStatusUpdateEvent(reqCtx(), a2atype.TaskStateFailed, nil))
	m.Update(streamDoneMsg{ch: m.streamCh})
	assert.Equal(t, []string{"go"}, *sent, "a failure holds the queue")
	assert.Contains(t, ansi.Strip(m.View()), "⏸ Queue paused after a failed turn · Enter to resume · ↑ edit")

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, []string{"go", "next"}, *sent, "Enter on an empty input resumes")
	assert.NotContains(t, ansi.Strip(m.View()), "paused")
}

// A failure with nothing queued leaves no pause behind for later messages.
func TestFailureWithEmptyQueueDoesNotPause(t *testing.T) {
	m, sent, end := queueChat(t)
	enter(m, "go")
	m.appendEvent(a2atype.NewStatusUpdateEvent(reqCtx(), a2atype.TaskStateFailed, nil))
	m.Update(streamDoneMsg{ch: m.streamCh})
	assert.Empty(t, m.queuePaused)
	enter(m, "again")
	enter(m, "queued")
	end()
	assert.Equal(t, []string{"go", "again", "queued"}, *sent)
}
