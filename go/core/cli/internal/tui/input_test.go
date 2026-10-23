package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A paste is input to edit, newline keys add lines, and only Enter sends.
func TestInputPasteAndNewlines(t *testing.T) {
	m, sent := sizedChat(t)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("first\r\nsecond\x1b[31m red\x1b[0m\x07\n\tthird"), Paste: true})
	assert.Equal(t, "first\nsecond red\n    third", m.input.Value(), "escape sequences and control characters go; tabs become spaces in the input")
	assert.Empty(t, *sent, "a pasted newline never sends")

	for _, key := range []tea.KeyMsg{{Type: tea.KeyEnter, Alt: true}, {Type: tea.KeyCtrlJ}} {
		before := m.input.LineCount()
		m.Update(key)
		assert.Equal(t, before+1, m.input.LineCount(), "%s adds a line", key)
	}
	assert.Empty(t, *sent)

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.Len(t, *sent, 1, "Enter sends")
	assert.Equal(t, "first\nsecond red\n    third", strings.TrimSpace((*sent)[0].Message.Parts[0].Text()))
}

// The input grows to show every line and the transcript gives up rows, down
// to a few; the chat stays its given height, with a divider above the input.
func TestInputGrowsWithItsLines(t *testing.T) {
	m, _ := sizedChat(t) // 100x40
	view := func() []string { return strings.Split(ansi.Strip(m.View()), "\n") }
	rows := view()
	assert.Equal(t, 40, len(rows))
	assert.True(t, strings.HasPrefix(rows[len(rows)-2], "▔▔▔▔"), "a bar sits right above the input")
	assert.Equal(t, "> Type a message (Enter to send)", strings.TrimRight(rows[len(rows)-1], " "))
	transcript := m.vp.Height

	m.input.SetValue("one\ntwo\nthree\nfour")
	rows = view()
	assert.Equal(t, 40, len(rows))
	assert.Equal(t, []string{"> one", "  two", "  three", "  four"}, trimAll(rows[len(rows)-4:]), "later lines align under the first line's text")
	assert.Equal(t, transcript-3, m.vp.Height)

	m.input.SetValue(strings.Repeat("line\n", 60) + "end")
	rows = view()
	assert.Equal(t, 40, len(rows), "the frame keeps its height")
	assert.Equal(t, minTranscriptRows, m.vp.Height, "the transcript keeps a few rows")
	assert.Equal(t, "  end", strings.TrimRight(rows[len(rows)-1], " "), "the cursor's line shows")

	m.input.SetValue(strings.Repeat("word ", 50))
	view()
	assert.Equal(t, 3, m.input.Height(), "a long line wraps onto more rows")
}

func trimAll(rows []string) []string {
	for i, row := range rows {
		rows[i] = strings.TrimRight(row, " ")
	}
	return rows
}

// keyRecorder collects the keys a program reads.
type keyRecorder struct{ keys chan tea.KeyMsg }

func (r keyRecorder) Init() tea.Cmd { return nil }
func (r keyRecorder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		r.keys <- key
	}
	return r, nil
}
func (r keyRecorder) View() string { return "" }

// Kitty keyboard reports reach bubbletea as keys it knows: Shift+Enter as
// alt+enter, and ctrl+c, esc, arrows and pastes as before, even split across reads.
func TestKittyInputKeys(t *testing.T) {
	kittyKeys.Store(false)
	t.Cleanup(func() { kittyKeys.Store(false) })
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	recorder := keyRecorder{keys: make(chan tea.KeyMsg, 16)}
	program := tea.NewProgram(recorder, tea.WithInput(&kittyInput{File: reader}), tea.WithOutput(os.Stderr), tea.WithoutRenderer(), tea.WithoutSignalHandler())
	done := make(chan struct{})
	go func() { _, _ = program.Run(); close(done) }()
	// Quit, unlike a cancelled context or Kill, waits for the read loop before closing its reader.
	t.Cleanup(func() { program.Quit(); <-done; _ = writer.Close(); _ = reader.Close() })

	next := func(raw ...string) string {
		for _, chunk := range raw {
			_, err := writer.WriteString(chunk)
			require.NoError(t, err)
			time.Sleep(20 * time.Millisecond)
		}
		select {
		case key := <-recorder.keys:
			return key.String()
		case <-time.After(2 * time.Second):
			t.Fatalf("no key for %q", raw)
			return ""
		}
	}
	assert.Equal(t, "alt+enter", next("\x1b[13;2u"), "Shift+Enter")
	assert.Equal(t, "alt+enter", next("\x1b[13;", "2u"), "split across reads")
	assert.Equal(t, "ctrl+c", next("\x1b[99;5u"))
	assert.Equal(t, "esc", next("\x1b[27u"))
	assert.Equal(t, "shift+tab", next("\x1b[9;2u"))
	assert.Equal(t, "alt+x", next("\x1b[120;3u"))
	assert.Equal(t, "up", next("\x1b[A"), "legacy keys pass through")
	assert.Equal(t, "enter", next("\r"))
	assert.Equal(t, "[a\nb]", next("\x1b[200~a\nb\x1b[201~"), "bracketed paste")

	_, _ = writer.WriteString("\x1b[?1u")
	time.Sleep(50 * time.Millisecond)
	assert.True(t, kittyKeys.Load(), "the query's answer says Shift+Enter works")
	assert.Equal(t, "a", next("a"), "the answer itself is not a key")
}

// Up and down move between a multi-line input's lines, and scroll the transcript past its ends.
func TestArrowsInMultiLineInput(t *testing.T) {
	m, _ := sizedChat(t)
	for i := range 80 {
		m.appendLine(strings.Repeat("x", i%5+1))
	}
	m.input.SetValue("one\ntwo\nthree")
	m.View()
	bottom := m.vp.YOffset
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 0, m.input.Line(), "up walks to the first line")
	assert.Equal(t, bottom, m.vp.YOffset, "the transcript holds still")
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Less(t, m.vp.YOffset, bottom, "past the first line, up scrolls the transcript")
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 1, m.input.Line())

	m.input.SetValue("single")
	m.View() // the shorter input gives the transcript its rows back
	m.vp.SetYOffset(m.vp.YOffset - 5)
	offset := m.vp.YOffset
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Greater(t, m.vp.YOffset, offset, "a one-line input leaves the arrows to the transcript")
}

func TestInputCursorIsSolid(t *testing.T) {
	m, _ := sizedChat(t)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	assert.Equal(t, cursor.CursorStatic, m.input.Cursor.Mode())
}

// The bar above the input is the chat box's border colour, focused or not.
func TestDividerMatchesTheBorder(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(0) // termenv.TrueColor, so styles emit escapes
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	m, _ := sizedChat(t)
	rule := strings.Repeat("▔", 100)
	for _, focused := range []bool{true, false} {
		m.unfocused = !focused
		want := lipgloss.NewStyle().Foreground(borderColor(focused)).Render(rule)
		assert.Contains(t, m.View(), want, "focused %v", focused)
		box := panelBox(panelChat, "x", 20, 5, focused)
		color, _, _ := strings.Cut(lipgloss.NewStyle().Foreground(borderColor(focused)).Render("x"), "x")
		assert.Contains(t, box, color+"╭", "the border starts in the same colour")
	}
}

// typeKeys sends text one key at a time, drawing after each as the program does; "\n" is ctrl+j.
func typeKeys(m *chatModel, text string) {
	for _, r := range text {
		if r == '\n' {
			m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
		} else {
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		m.View()
	}
}

// Every line of the input shows, however it got there: typed newlines, a paste, or a long line that wraps.
func TestInputShowsEveryLine(t *testing.T) {
	inputRows := func(m *chatModel, n int) []string {
		rows := strings.Split(ansi.Strip(m.View()), "\n")
		return trimAll(rows[len(rows)-n:])
	}
	t.Run("typed newlines", func(t *testing.T) {
		m, _ := sizedChat(t)
		typeKeys(m, "one\ntwo\nthree")
		assert.Equal(t, []string{"> one", "  two", "  three"}, inputRows(m, 3))
	})
	t.Run("a pasted block", func(t *testing.T) {
		m, _ := sizedChat(t)
		typeKeys(m, "start ")
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a\nb\nc"), Paste: true})
		assert.Equal(t, []string{"> start a", "  b", "  c"}, inputRows(m, 3))
	})
	t.Run("a long line wraps", func(t *testing.T) {
		m, _ := sizedChat(t)
		typeKeys(m, strings.Repeat("word ", 45))
		rows := inputRows(m, 3)
		assert.True(t, strings.HasPrefix(rows[0], "> word"), "the first row stays in view: %q", rows)
		assert.Equal(t, 3, m.input.Height())
		typeKeys(m, "end")
		assert.True(t, strings.HasSuffix(m.input.Value(), "word end"), "typing goes on at the cursor")
		assert.True(t, strings.HasSuffix(inputRows(m, 1)[0], "word end"))
	})
	t.Run("a capped input that shrinks back shows from its top", func(t *testing.T) {
		m, _ := sizedChat(t)
		typeKeys(m, strings.Repeat("x\n", 40)+"last")
		assert.Equal(t, minTranscriptRows, m.vp.Height, "capped, the transcript keeps a few rows")
		for range 36 * 2 {
			m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
			m.View()
		}
		rows := inputRows(m, m.input.Height())
		assert.Equal(t, "> x", rows[0], "%q", rows)
		lines := strings.Split(m.input.Value(), "\n")
		assert.Len(t, rows, len(lines), "every line shows")
		assert.Equal(t, strings.TrimRight("  "+lines[len(lines)-1], " "), rows[len(rows)-1])
	})
}

// Up and down walk a wrapped line's rows before they leave the input.
func TestArrowsWalkWrappedRows(t *testing.T) {
	m, _ := sizedChat(t)
	for range 80 {
		m.appendLine("x")
	}
	typeKeys(m, strings.Repeat("word ", 45)) // one line, three rows at this width
	require.Equal(t, 3, m.input.Height())
	bottom := m.vp.YOffset
	for range 2 {
		m.Update(tea.KeyMsg{Type: tea.KeyUp})
	}
	assert.True(t, m.inputAtFirstRow(), "up walked to the first row")
	assert.Equal(t, bottom, m.vp.YOffset, "the transcript holds still")
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Less(t, m.vp.YOffset, bottom, "past the first row, up leaves the input")

	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.False(t, m.inputAtFirstRow(), "down walks back into the wrapped rows")
	assert.Equal(t, 0, m.input.Line(), "still the one line")
}
