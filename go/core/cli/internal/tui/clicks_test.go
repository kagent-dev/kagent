package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cellOf finds text in the chat's drawn view and returns its first cell.
func cellOf(t *testing.T, m *chatModel, text string) (int, int) {
	t.Helper()
	for y, row := range strings.Split(m.View(), "\n") {
		plain := ansi.Strip(row)
		if i := strings.Index(plain, text); i >= 0 {
			return ansi.StringWidth(plain[:i]), y
		}
	}
	t.Fatalf("%q is not drawn", text)
	return 0, 0
}

func click(m *chatModel, x, y int) tea.Cmd {
	m.mouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}, x, y)
	return m.mouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease}, x, y)
}

func clickOn(t *testing.T, m *chatModel, text string) tea.Cmd {
	t.Helper()
	x, y := cellOf(t, m, text)
	return click(m, x, y)
}

func seqTool(command string) *block {
	return &block{tool: &toolEntry{name: "bash", args: map[string]any{"command": command}, response: map[string]any{"output": "1\n2\n3\n4\n5"}, done: true}}
}

func TestClickTogglesOneToolBlock(t *testing.T) {
	m, _ := sizedChat(t)
	first, second := seqTool("seq a"), seqTool("seq b")
	m.appendBlock(first)
	m.appendBlock(second)

	clickOn(t, m, "3") // the first block's output
	assert.True(t, first.expanded, "a click on a block's output toggles it")
	assert.False(t, second.expanded, "and no other block")
	assert.Equal(t, 1, strings.Count(transcript(m), "+2 lines"))

	clickOn(t, m, "seq b")
	assert.True(t, second.expanded)
	clickOn(t, m, "seq a")
	assert.False(t, first.expanded, "a second click collapses it")
}

func TestClickExpandsAThought(t *testing.T) {
	m, _ := sizedChat(t)
	m.appendBlock(&block{thought: &thoughtEntry{text: "Weigh both.", start: time.Now(), end: time.Now()}})
	clickOn(t, m, "Thought")
	assert.Contains(t, ansi.Strip(transcript(m)), "│ Weigh both.")
}

// The pill is a button too.
func TestClickOnThePillJumps(t *testing.T) {
	m, _ := sizedChat(t)
	for range 60 {
		m.appendLine("line")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	clickOn(t, m, "Jump to latest")
	assert.True(t, m.vp.AtBottom())
}

// A click opens only web links, through the one launcher; file:// and other schemes could launch something.
func TestClickOpensOnlyWebLinks(t *testing.T) {
	var opened []string
	stubLauncher(t, func(url string) error { opened = append(opened, url); return nil })

	for _, target := range []string{"https://a.dev", "http://a.dev"} {
		assert.True(t, openableLink(target), target)
	}
	for _, target := range []string{"file:///tmp/x.command", "vscode://file/x", "ssh://host", "javascript:alert(1)"} {
		assert.False(t, openableLink(target), target)
	}
	assert.Equal(t, "see file:///tmp/x.command", hyperlinks("see file:///tmp/x.command"), "text never gets a file:// link")

	m, _ := sizedChat(t)
	m.appendBlock(&block{text: "Docs: https://kagent.dev/docs here", markdown: true})
	m.appendLine("run " + ansi.SetHyperlink("file:///tmp/x.command") + "this" + ansi.ResetHyperlink())
	assert.Nil(t, clickOn(t, m, "this"), "a file:// link in the transcript does nothing")
	assert.Nil(t, clickOn(t, m, "Docs"), "text beside a link opens nothing")
	cmd := clickOn(t, m, "kagent.dev")
	require.NotNil(t, cmd)
	cmd()
	assert.Equal(t, []string{"https://kagent.dev/docs"}, opened)
}

// Under go test, an unstubbed launch panics rather than opening a window.
func TestLauncherRefusesUnderTest(t *testing.T) {
	assert.Panics(t, func() { _ = openURL("https://a.dev") })
}

// The workspace hands clicks in the chat panel to the chat.
func TestWorkspaceRoutesChatClicks(t *testing.T) {
	m := openWorkspace(t)
	tool := seqTool("seq a")
	m.chat.appendBlock(tool)
	m.View()

	x, y := cellOf(t, m.chat, "seq a")
	ox, oy := m.chatOrigin()
	for _, action := range []tea.MouseAction{tea.MouseActionPress, tea.MouseActionRelease} {
		m.Update(tea.MouseMsg{X: x + ox, Y: y + oy, Button: tea.MouseButtonLeft, Action: action})
	}
	assert.True(t, tool.expanded)
	assert.Equal(t, panelChat, m.focus)
}
