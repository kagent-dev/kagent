package tui

import (
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func press(m *chatModel, keys ...tea.KeyType) {
	for _, key := range keys {
		m.Update(tea.KeyMsg{Type: key})
		m.View()
	}
}

// send enters a prompt and ends its turn.
func send(m *chatModel, text string) {
	enter(m, text)
	m.Update(streamDoneMsg{ch: m.streamCh})
}

// Up from the input's first row recalls earlier prompts; down walks forward to the draft, which is kept.
func TestUpRecallsPromptHistory(t *testing.T) {
	m, _ := sizedChat(t)
	send(m, "first")
	send(m, "second\nmulti-line")
	typeKeys(m, "draft")

	press(m, tea.KeyUp)
	assert.Equal(t, "second\nmulti-line", m.input.Value())
	assert.True(t, m.inputAtFirstRow(), "a recalled prompt opens at its top, so up keeps going back")
	assert.Equal(t, 2, m.input.Height(), "it shows whole")
	press(m, tea.KeyUp)
	assert.Equal(t, "first", m.input.Value())
	press(m, tea.KeyUp)
	assert.Equal(t, "first", m.input.Value(), "nothing older")

	press(m, tea.KeyDown)
	assert.Equal(t, "second\nmulti-line", m.input.Value())
	assert.True(t, m.inputAtLastRow(), "going forward opens at the end, so down keeps going forward")
	press(m, tea.KeyUp)
	assert.Equal(t, "second\nmulti-line", m.input.Value(), "up moves within the recalled lines first")
	press(m, tea.KeyDown, tea.KeyDown)
	assert.Equal(t, "draft", m.input.Value(), "the draft comes back last")

	press(m, tea.KeyUp)
	send(m, "second, edited")
	press(m, tea.KeyUp)
	assert.Equal(t, "second, edited", m.input.Value(), "a sent prompt is the newest")
}

// Queued messages come back before history does.
func TestUpPullsTheQueueBeforeHistory(t *testing.T) {
	m, _, _ := queueChat(t)
	enter(m, "running")
	enter(m, "queued")
	press(m, tea.KeyUp)
	assert.Equal(t, "queued", m.input.Value())
	m.input.Reset()
	press(m, tea.KeyUp)
	assert.Equal(t, "running", m.input.Value())
}

// A resumed session's past prompts are its history.
func TestResumedPromptsSeedHistory(t *testing.T) {
	m, _ := sizedChat(t)
	m.AppendHistoryTask(historyTask(1, artifactOf(a2atype.NewTextPart("reply"))))
	m.AppendHistoryTask(historyTask(2))
	press(m, tea.KeyUp)
	assert.Equal(t, "prompt 2", m.input.Value())
	press(m, tea.KeyUp)
	assert.Equal(t, "prompt 1", m.input.Value())
}
