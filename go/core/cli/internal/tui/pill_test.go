package tui

import (
	"fmt"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

// Scrolled up, the transcript holds still as output streams in, and a pill
// counts the new lines; end or ctrl+end jumps back to the latest.
func TestJumpToLatestPill(t *testing.T) {
	m, _ := sizedChat(t)
	for i := range 60 {
		m.appendLine(fmt.Sprintf("line %d", i))
	}
	assert.NotContains(t, ansi.Strip(m.View()), "Jump to latest", "hidden at the bottom")

	m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	assert.Contains(t, ansi.Strip(m.View()), "[ ↓ Jump to latest · end ]")
	offset := m.vp.YOffset

	m.setWorkingTime(time.Time{})
	first := a2atype.NewArtifactEvent(reqCtx(), a2atype.NewTextPart("streamed\n\nmore\n\nand more"))
	m.appendEvent(first)
	assert.Equal(t, offset, m.vp.YOffset, "no auto-follow while scrolled up")
	assert.Contains(t, ansi.Strip(m.View()), "[ ↓ Answering… · end ]", "out of sight, the turn's status shows on the pill")
	m.endStream()
	assert.Contains(t, ansi.Strip(m.View()), "[ ↓ 6 new lines · end ]")

	m.input.SetValue("draft")
	assert.Contains(t, ansi.Strip(m.View()), "· ctrl+end ]", "end edits the input while it has text")
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	assert.False(t, m.vp.AtBottom())
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlEnd})
	assert.True(t, m.vp.AtBottom(), "ctrl+end jumps to the latest")
	assert.NotContains(t, ansi.Strip(m.View()), "new lines")

	m.appendEvent(a2atype.NewArtifactUpdateEvent(reqCtx(), first.Artifact.ID, a2atype.NewTextPart("\n\nfollowing")))
	assert.True(t, m.vp.AtBottom(), "at the bottom, output is followed")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	assert.True(t, m.vp.AtBottom(), "typed letters never scroll")
}
