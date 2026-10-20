package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

// The frame fills the window exactly, so the terminal never scrolls and shows its scrollbar.
func TestFrameFillsTheWindow(t *testing.T) {
	session := readySession("44444444-4444-4444-4444-444444444444", "reporter")
	for _, width := range []int{40, 60, 80, 100, 120, 160, 220} {
		for _, height := range []int{12, 20, 30, 50} {
			t.Run(fmt.Sprintf("%dx%d", width, height), func(t *testing.T) {
				m := testWorkspace(t, &fakeLister{pages: []*apiv1.ListSessionsResponse{page("", session)}})
				loaded(m)
				m.selectSession(session)
				m.Update(tea.WindowSizeMsg{Width: width, Height: height})
				m.chat.appendUser("hello")
				// A long status line must not wrap past the frame.
				m.status = "Failed to load history: the gateway did not answer in time, so the transcript shows only what streamed."
				view := m.View()
				assert.Equal(t, height, lineCount(view), "rows")
				for i, row := range strings.Split(view, "\n") {
					assert.LessOrEqual(t, ansi.StringWidth(row), width, "row %d is too wide", i)
				}
			})
		}
	}
}
