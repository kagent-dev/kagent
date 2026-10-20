package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubClipboard records what a copy puts on the clipboard.
func stubClipboard(t *testing.T) *[]string {
	t.Helper()
	var copied []string
	previous := copyText
	copyText = func(text string) error { copied = append(copied, text); return nil }
	t.Cleanup(func() { copyText = previous })
	return &copied
}

func drag(m *chatModel, from, to cell) tea.Cmd {
	m.mouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}, from.x, from.y)
	m.mouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion}, to.x, to.y)
	return m.mouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease}, to.x, to.y)
}

// rowsOf finds the first and last drawn rows holding first and last.
func rowsOf(t *testing.T, m *chatModel, first, last string) (cell, cell) {
	t.Helper()
	fx, fy := cellOf(t, m, first)
	lx, ly := cellOf(t, m, last)
	return cell{fx, fy}, cell{lx + ansi.StringWidth(last) - 1, ly}
}

func TestSelectionColumns(t *testing.T) {
	s := selection{from: cell{5, 3}, to: cell{2, 1}} // dragged upward
	left, right, ok := s.columns(1, 80)
	assert.Equal(t, []any{2, 80, true}, []any{left, right, ok}, "the first row runs to the edge")
	left, right, _ = s.columns(2, 80)
	assert.Equal(t, []int{0, 80}, []int{left, right})
	left, right, _ = s.columns(3, 80)
	assert.Equal(t, []int{0, 6}, []int{left, right}, "the last row stops at the pointer, inclusive")
	_, _, ok = s.columns(4, 80)
	assert.False(t, ok)
}

// A drag copies plain text: no styling, no gutter, and a soft-wrapped message rejoined.
func TestDragCopiesPlainText(t *testing.T) {
	copied := stubClipboard(t)
	m, _ := sizedChat(t)
	message := "Please read every file under the cmd folder and tell me which ones still use the old config loader before we cut a release"
	m.appendUser(message)
	m.appendBlock(seqTool("seq 5"))
	m.View()

	from, to := rowsOf(t, m, "Please", "release")
	require.Greater(t, to.y, from.y, "the message wraps")
	cmd := drag(m, from, to)
	require.NotNil(t, cmd)
	msg := cmd()
	assert.Equal(t, []string{message}, *copied)
	assert.True(t, m.hasSelection())
	assert.Contains(t, ansi.Strip(m.View()), message[:20])
	assert.Contains(t, m.View(), "\x1b[7m", "the selection is drawn in reverse video")

	_, done := m.Update(msg)
	require.NotNil(t, done, "the notice clears itself")
	assert.Contains(t, ansi.Strip(m.View()), fmt.Sprintf("Copied %d characters", len(message)))
	assert.NotContains(t, transcript(m), "Copied", "the notice is not part of the transcript")
	m.Update(noticeDoneMsg{id: m.noticeID})
	assert.NotContains(t, ansi.Strip(m.View()), "Copied")

	from, to = rowsOf(t, m, "│ 1", "│ 3")
	from.x = 0
	drag(m, from, to)()
	assert.Equal(t, "1\n2\n3", (*copied)[1], "the gutter is not text")
	assert.NotContains(t, strings.Join(*copied, ""), "\x1b")
}

// ctrl+c copies a selection once instead of quitting; the next press cancels as usual.
func TestCtrlCCopiesASelection(t *testing.T) {
	copied := stubClipboard(t)
	m := openWorkspace(t)
	m.chat.appendUser("copy me please")
	m.View()
	from, to := rowsOf(t, m.chat, "copy", "please")
	drag(m.chat, from, to)()

	cmd, handled := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	require.True(t, handled)
	require.NotNil(t, cmd)
	_, quit := cmd().(tea.QuitMsg)
	assert.False(t, quit, "ctrl+c copies rather than quits")
	assert.Equal(t, []string{"copy me please", "copy me please"}, *copied)

	assert.False(t, m.chat.hasSelection(), "a copied selection is cleared")
	// The first press returns only the notice's timer, so it is not run here.
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.Contains(t, ansi.Strip(m.View()), "Press ctrl+c again to exit")
	cmd, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	_, quit = cmd().(tea.QuitMsg)
	assert.True(t, quit, "a second ctrl+c soon after quits")
}

// screenCell finds text on the workspace's drawn screen.
func screenCell(t *testing.T, m *workspaceModel, text string) cell {
	t.Helper()
	for y, row := range strings.Split(ansi.Strip(m.View()), "\n") {
		if i := strings.Index(row, text); i >= 0 {
			return cell{ansi.StringWidth(row[:i]), y}
		}
	}
	t.Fatalf("%q is not on screen", text)
	return cell{}
}

func screenDrag(m *workspaceModel, from, to cell) tea.Cmd {
	m.Update(tea.MouseMsg{X: from.x, Y: from.y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m.Update(tea.MouseMsg{X: to.x, Y: to.y, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	_, cmd := m.Update(tea.MouseMsg{X: to.x, Y: to.y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	return cmd
}

// A drag that starts outside the chat's rows selects across the screen, row by
// row, and copies plain text without styling or panel borders.
func TestDragSelectsAcrossTheScreen(t *testing.T) {
	copied := stubClipboard(t)
	m := openWorkspace(t)

	// From the version line, through the sidebar, to the chat's title row.
	from, to := screenCell(t, m, "kagent"), screenCell(t, m, "[0] reporter")
	to.x += len("[0] reporter") - 1
	m.Update(tea.MouseMsg{X: from.x, Y: from.y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m.Update(tea.MouseMsg{X: to.x, Y: to.y, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	assert.Contains(t, m.View(), "\x1b[7mkagent", "the selection shows as it is dragged")
	_, cmd := m.Update(tea.MouseMsg{X: to.x, Y: to.y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	require.NotNil(t, cmd)
	m.Update(cmd())
	require.Len(t, *copied, 1)
	assert.Equal(t, "kagent  dev\n[1] Namespaces  [0] reporter", (*copied)[0])
	assert.Contains(t, ansi.Strip(m.View()), "Copied ", "the chat's notice reports it, wherever focus is")

	// The footer, and ctrl+c copies the selection again.
	from, to = screenCell(t, m, "details: ctrl+d"), screenCell(t, m, "quit: ctrl+c")
	to.x += len("quit: ctrl+c") - 1
	runBatch(screenDrag(m, from, to))
	cmd, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	require.NotNil(t, cmd)
	cmd()
	require.Len(t, *copied, 3)
	assert.True(t, strings.HasPrefix((*copied)[2], "details: ctrl+d"), (*copied)[2])
	assert.True(t, strings.HasSuffix((*copied)[2], "quit: ctrl+c"), (*copied)[2])

	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.NotContains(t, m.View(), "\x1b[7m", "esc clears it")

	// The chat's own status line copies too.
	m.chat.View()
	from, to = rowsOf(t, m.chat, "Copied", "characters")
	runBatch(drag(m.chat, from, to))
	assert.Regexp(t, `^Copied \d+ characters$`, (*copied)[len(*copied)-1])
}
