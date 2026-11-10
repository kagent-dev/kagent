package tui

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
)

// cell is a position in the chat's drawn view: column, then row.
type cell struct{ x, y int }

// selection is a drag across the chat's drawn rows, from where it started to where it is now.
type selection struct {
	from, to cell
}

// ordered returns the selection's ends in reading order.
func (s selection) ordered() (cell, cell) {
	if s.to.y < s.from.y || (s.to.y == s.from.y && s.to.x < s.from.x) {
		return s.to, s.from
	}
	return s.from, s.to
}

// columns is the span [start, end) a selection covers on row y of a view width wide.
func (s selection) columns(y, width int) (int, int, bool) {
	start, end := s.ordered()
	if y < start.y || y > end.y {
		return 0, 0, false
	}
	left, right := 0, width
	if y == start.y {
		left = start.x
	}
	if y == end.y {
		right = end.x + 1
	}
	return left, right, left < right
}

// copiedMsg reports a finished copy.
type copiedMsg struct {
	chars int
	err   error
}

// copyText puts text on the system clipboard; tests replace it. Over SSH the
// local tools would fill the remote machine's clipboard, so OSC 52 also goes to
// the terminal, which sets the clipboard where the user sits if it supports that.
var copyText = func(text string) error {
	if env.SSHConnection.Get() != "" {
		writeOSC52(text)
	}
	var tools [][]string
	switch runtime.GOOS {
	case "darwin":
		tools = [][]string{{"pbcopy"}}
	default:
		tools = [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}}
	}
	for _, tool := range tools {
		if _, err := exec.LookPath(tool[0]); err != nil {
			continue
		}
		cmd := exec.Command(tool[0], tool[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	if env.SSHConnection.Get() == "" {
		writeOSC52(text)
	}
	return nil
}

// writeOSC52 asks the terminal to set its clipboard; it draws nothing.
var writeOSC52 = func(text string) {
	_, _ = fmt.Fprint(os.Stdout, ansi.SetSystemClipboard(text))
}

// selected marks selected cells in reverse video, which keeps the text's own
// contrast in either theme. It is written directly, since every terminal has it.
func selected(text string) string {
	return "\x1b[7m" + text + "\x1b[27m"
}

// highlightRows draws a selection over rendered rows in place.
func highlightRows(rows []string, s *selection, width int) []string {
	if s == nil {
		return rows
	}
	for y, row := range rows {
		left, right, ok := s.columns(y, width)
		if !ok {
			continue
		}
		right = min(right, ansi.StringWidth(row))
		if left >= right {
			continue
		}
		rows[y] = ansi.Cut(row, 0, left) + selected(ansi.Strip(ansi.Cut(row, left, right))) + ansi.Cut(row, right, ansi.StringWidth(row))
	}
	return rows
}

// gutter is the bar some blocks draw before their rows, which is not text.
var gutter = regexp.MustCompile(`^\s*│ ?`)

// selectedText is the plain text under the selection: no styling or gutters,
// and rows the wrapper broke joined back into one line.
func (m *chatModel) selectedText() string {
	if m.selection == nil {
		return ""
	}
	width := max(m.vp.Width-2, 1)
	start, end := m.selection.ordered()
	var rows []string
	var full []bool // the selection reaches the row's end, so it may run on into the next
	for y := start.y; y <= end.y && y < len(m.geo.view); y++ {
		left, right, ok := m.selection.columns(y, m.vp.Width)
		if !ok {
			continue
		}
		text := ansi.Strip(m.rowAt(y))
		rows = append(rows, ansi.Strip(ansi.Cut(text, left, right)))
		full = append(full, y < end.y)
	}
	var out strings.Builder
	for i, row := range rows {
		trimmed := strings.TrimRight(gutter.ReplaceAllString(row, ""), " ")
		if i == 0 {
			out.WriteString(strings.TrimLeft(trimmed, " "))
			continue
		}
		previous := strings.TrimRight(rows[i-1], " ")
		next := strings.TrimLeft(trimmed, " ")
		switch {
		case !full[i-1] || !softWrapped(previous, next, width):
			out.WriteString("\n" + trimmed)
		case ansi.StringWidth(previous) >= width:
			out.WriteString(next) // a hard break inside a word, such as a long URL
		default:
			out.WriteString(" " + next)
		}
	}
	return out.String()
}

// softWrapped guesses whether a row ended only because the wrapper ran out of
// room: it is full, or the next row's first word would not have fit after it.
// The renderers do not record their wraps, so a short line that happens to end
// near the edge reads as wrapped too.
func softWrapped(row, next string, width int) bool {
	row = strings.TrimSpace(gutter.ReplaceAllString(row, ""))
	if row == "" || next == "" {
		return false
	}
	word, _, _ := strings.Cut(next, " ")
	return ansi.StringWidth(strings.TrimRight(row, " "))+1+ansi.StringWidth(word) > width-4
}

// copySelection copies the selected text and reports how much, never echoing the text itself.
func (m *chatModel) copySelection() tea.Cmd {
	text := m.selectedText()
	if text == "" {
		return nil
	}
	return func() tea.Msg {
		return copiedMsg{chars: utf8.RuneCountInString(text), err: copyText(text)}
	}
}

// copiedNotice reports a finished copy, never echoing the text itself.
func (m *chatModel) copiedNotice(msg copiedMsg) tea.Cmd {
	if msg.err != nil {
		return m.showNotice("Copy failed: " + msg.err.Error())
	}
	return m.showNotice("Copied " + plural(msg.chars, "character"))
}

// hasSelection reports whether a drag left text selected.
func (m *chatModel) hasSelection() bool {
	return m != nil && m.selection != nil && m.selection.from != m.selection.to
}

// boxRun is a run of panel borders and rules, with the padding beside them.
var boxRun = regexp.MustCompile(` *[│╭╮╰╯─]+ *`)

// screenText is the plain text a selection covers on whole-screen rows: one
// line per row, with styling, panel borders and blank rows left out.
func screenText(rows []string, s selection, width int) string {
	start, end := s.ordered()
	var lines []string
	for y := start.y; y <= end.y && y < len(rows); y++ {
		left, right, ok := s.columns(y, width)
		if !ok {
			continue
		}
		text := ansi.Strip(ansi.Cut(rows[y], left, right))
		if text = strings.TrimSpace(boxRun.ReplaceAllString(text, "  ")); text != "" {
			lines = append(lines, text)
		}
	}
	return strings.Join(lines, "\n")
}
