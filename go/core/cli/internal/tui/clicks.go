package tui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// span is the content lines [start, end) one block took; block is nil for streaming text.
type span struct {
	start, end int
	block      *block
}

// viewGeometry is where View last drew the transcript: its first row, how many
// rows show, the notice row and the pill's first column on it (-1 when absent), and every row drawn.
type viewGeometry struct {
	top, rows, status, pill int
	view                    []string
}

// launcher is a test's stand-in for the browser; nil outside tests.
var launcher atomic.Pointer[func(url string) error]

// openURL opens a web page in the default browser. Under go test it panics
// unless stubLauncher installed a stand-in, so no test opens a real window.
func openURL(url string) error {
	if stub := launcher.Load(); stub != nil {
		return (*stub)(url)
	}
	if testing.Testing() {
		panic(fmt.Sprintf("openURL(%q) under go test would open a real window; call stubLauncher", url))
	}
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.CommandContext(context.Background(), name, url).Run()
}

// stubLauncher sends every openURL to fn instead until the test ends.
func stubLauncher(t testing.TB, fn func(url string) error) {
	t.Helper()
	launcher.Store(&fn)
	t.Cleanup(func() { launcher.Store(nil) })
}

// openableLink reports whether a click opens a link: web pages only. Any
// other scheme, file:// included, could launch something, so it never opens.
func openableLink(target string) bool {
	return strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "http://")
}

// linkAt is the OSC 8 link target under column col of a drawn row, or "".
// Drawn text is sanitized, so every link in it is one kagent rendered.
func linkAt(row string, col int) string {
	pos, open := 0, ""
	for {
		loc := osc8.FindStringSubmatchIndex(row)
		text := row
		if loc != nil {
			text = row[:loc[0]]
		}
		width := ansi.StringWidth(text)
		if open != "" && col >= pos && col < pos+width {
			return open
		}
		pos += width
		if loc == nil {
			return ""
		}
		open, row = row[loc[2]:loc[3]], row[loc[1]:]
	}
}

// hit finds the block on row y of the chat's last View.
func (m *chatModel) hit(y int) *block {
	if y < m.geo.top || y >= m.geo.top+m.geo.rows {
		return nil
	}
	line := m.vp.YOffset + y - m.geo.top
	for _, s := range m.spans {
		if line >= s.start && line < s.end {
			return s.block
		}
	}
	return nil
}

// rowAt is the drawn row under y: a header row, or a transcript line.
func (m *chatModel) rowAt(y int) string {
	// The header, and the rows below the transcript, are drawn rows as they are.
	if y >= 0 && (y < m.geo.top || y >= m.geo.top+m.geo.rows) && y < len(m.geo.view) {
		return m.geo.view[y]
	}
	if line := m.vp.YOffset + y - m.geo.top; y < m.geo.top+m.geo.rows && line >= 0 && line < len(m.lines) {
		return m.lines[line]
	}
	return ""
}

// mouse handles a left-button event at (x, y) of the chat. A drag selects
// text and copies it on release; a click jumps to the latest from the pill,
// opens the web link under it, or expands the tool call or thought it lands on.
func (m *chatModel) mouse(msg tea.MouseMsg, x, y int) tea.Cmd {
	if msg.Button != tea.MouseButtonLeft && msg.Action != tea.MouseActionMotion {
		return nil
	}
	here := cell{x, y}
	switch msg.Action {
	case tea.MouseActionPress:
		m.selection, m.dragFrom = nil, &here
	case tea.MouseActionMotion:
		if m.dragFrom != nil && here != *m.dragFrom {
			m.selection = &selection{from: *m.dragFrom, to: here}
		}
	case tea.MouseActionRelease:
		m.dragFrom = nil
		if m.selection != nil {
			m.selection.to = here
			return m.copySelection()
		}
		if y == m.geo.status && m.geo.pill >= 0 && x >= m.geo.pill {
			m.jumpToLatest()
			return nil
		}
		if link := linkAt(m.rowAt(y), x); openableLink(link) {
			return func() tea.Msg {
				_ = openURL(link) // a failed open leaves nothing to show
				return nil
			}
		}
		if target := m.hit(y); target != nil && (target.tool != nil || target.thought != nil) {
			target.expanded = !target.expanded
			m.redraw()
		}
	}
	return nil
}

// redraw renders again without moving the scroll position.
func (m *chatModel) redraw() {
	offset := m.vp.YOffset
	m.render()
	m.vp.SetYOffset(offset)
}
