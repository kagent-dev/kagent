package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

// hiddenWorkspace opens a session in a workspace configured to start without its sidebar.
func hiddenWorkspace(t *testing.T) *workspaceModel {
	t.Helper()
	session := readySession("44444444-4444-4444-4444-444444444444", "reporter")
	m := testWorkspace(t, &fakeLister{pages: []*apiv1.ListSessionsResponse{page("", session)}})
	m.cfg.HideSidebar, m.sidebar = true, false
	loaded(m)
	m.selectSession(session)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return m
}

// runCmd runs a command and hands its message back to the workspace, as the program does.
func runCmd(m *workspaceModel, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if msg := cmd(); msg != nil {
		m.Update(msg)
	}
}

func TestSidebarStartsHiddenAndToggles(t *testing.T) {
	// Toggling writes nothing, here or under the user's config directory.
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("HOME", configDir)

	m := hiddenWorkspace(t)
	view := ansi.Strip(m.View())
	assert.NotContains(t, view, "Namespaces", "hidden from the start")
	assert.Contains(t, view, "sidebar: /sidebar")
	assert.NotContains(t, view, "focus: click, tab or 0-4", "no hints for panels that are not there")
	assert.Equal(t, 120-2, m.chat.vp.Width, "the chat takes the full width, less a margin each side")
	assert.Equal(t, panelChat, m.focus)

	_, handled := m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	assert.True(t, handled)
	assert.Equal(t, panelChat, m.focus, "tab has nowhere else to go")

	m.chat.input.SetValue("/sidebar")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	runCmd(m, cmd)
	assert.Contains(t, ansi.Strip(m.View()), "[1] Namespaces", "/sidebar brings it back")
	assert.Equal(t, 120-sidebarWidth-4, m.chat.vp.Width)
	assert.Empty(t, m.chat.blocks, "the command is not sent as a message")
	assert.Empty(t, m.chat.prompts)

	m.chat.input.SetValue("/sidebar")
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	runCmd(m, cmd)
	assert.NotContains(t, ansi.Strip(m.View()), "Namespaces", "and hides it again")

	entries, err := os.ReadDir(configDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "the toggle lasts only for the run")
}

// The default keeps today's layout.
func TestSidebarShownByDefault(t *testing.T) {
	m := openWorkspace(t)
	assert.Contains(t, ansi.Strip(m.View()), "[1] Namespaces")
}

// Only "/sidebar" is a command; other text starting with a slash is a message.
func TestOtherSlashTextIsSent(t *testing.T) {
	m, sent := sizedChat(t)
	enter(m, "/sidebars please")
	require.Len(t, *sent, 1)
	assert.Equal(t, "/sidebars please", (*sent)[0].Message.Parts[0].Text())
}

// With the sidebar hidden the frame still fills the window exactly.
func TestHiddenSidebarFrameFillsTheWindow(t *testing.T) {
	m := hiddenWorkspace(t)
	for _, width := range []int{40, 60, 80, 100, 120, 160, 220} {
		for _, height := range []int{12, 20, 30, 50} {
			m.Update(tea.WindowSizeMsg{Width: width, Height: height})
			m.chat.appendUser("hello")
			view := m.View()
			assert.Equal(t, height, lineCount(view), "%dx%d", width, height)
			for row := range strings.SplitSeq(view, "\n") {
				assert.LessOrEqual(t, ansi.StringWidth(row), width, "%dx%d", width, height)
			}
		}
	}
}

// Without the sidebar the chat has no box: a one-column margin each side, its
// header row and rule on top, and border-coloured bars above and below the input.
func TestHiddenSidebarChatHasNoBox(t *testing.T) {
	m := hiddenWorkspace(t)
	rows := strings.Split(ansi.Strip(m.View()), "\n")
	require.Len(t, rows, 30)
	view := strings.Join(rows, "\n")
	assert.NotContains(t, view, "╭")
	assert.NotContains(t, view, "│ ")
	assert.True(t, strings.HasPrefix(rows[1], " [0] reporter · 44444444"), "%q", rows[1])
	assert.True(t, strings.HasPrefix(rows[2], " ───"), "the rule under the header: %q", rows[2])
	footer := lineCount(m.footerView())
	input := trimAll(rows[len(rows)-footer-3 : len(rows)-footer])
	assert.Equal(t, []string{" " + strings.Repeat("▔", 118), " > Type a message (Enter to send)", " " + strings.Repeat("▁", 118)}, input)

	ox, oy := m.chatOrigin()
	assert.Equal(t, "[0] reporter", ansi.Cut(rows[oy], ox, ox+len("[0] reporter")), "clicks map onto the chat's cells")

	m.chat.input.SetValue("/sidebar")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	runCmd(m, cmd)
	assert.Contains(t, m.View(), "╭", "beside the sidebar, the chat keeps its box")
	assert.NotContains(t, ansi.Strip(m.View()), "▁▁▁", "and its border closes the input")
}
