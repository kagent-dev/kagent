package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// trueColor renders styles as 24-bit escapes against a dark or light background for the test.
func trueColor(t *testing.T, dark bool) {
	t.Helper()
	profile, background := lipgloss.ColorProfile(), lipgloss.HasDarkBackground()
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(dark)
	t.Cleanup(func() {
		lipgloss.SetColorProfile(profile)
		lipgloss.SetHasDarkBackground(background)
	})
}

const linkDark = "38;2;121;192;255"

// URLs and paths that start a word take the link colour and an underline; the styling around them comes back after.
func TestColorLinks(t *testing.T) {
	trueColor(t, true)
	body := "\x1b[38;2;230;237;243m"
	got := colorLinks(body + "read /etc/hosts, ~/notes.md and ./run.sh at https://a.dev/x. not and/or or 1/2" + "\x1b[0m")
	for _, link := range []string{"/etc/hosts", "~/notes.md", "./run.sh", "https://a.dev/x"} {
		assert.Contains(t, got, linkDark+";4m"+link+"\x1b[0m"+body, link)
	}
	assert.Contains(t, got, "/etc/hosts\x1b[0m"+body+",", "trailing punctuation is not part of the path")
	assert.Contains(t, got, "and/or or 1/2")
	assert.Equal(t, ansi.Strip(got), "read /etc/hosts, ~/notes.md and ./run.sh at https://a.dev/x. not and/or or 1/2")

	for _, code := range []string{"\x1b[38;2;255;200;0;48;5;236m/etc/hosts\x1b[0m", "\x1b[38;2;201;209;217mcat /etc/hosts\x1b[0m", "\x1b[38;2;196;196;196mcat /etc/hosts\x1b[0m"} {
		assert.Equal(t, code, colorLinks(code), "code spans and blocks keep their colours")
	}
}

// Body text is bright, bold brighter still, and links, headings and paths take theme colours.
func TestMarkdownColors(t *testing.T) {
	trueColor(t, true)
	renderer, err := markdownRenderer(80)
	require.NoError(t, err)
	got := colorLinks(renderMarkdown(renderer, "## Plan\n\nRun it **now**, see [docs](https://kagent.dev) and /etc/hosts.\n\n- one"))
	assert.Contains(t, got, "38;2;230;237;243", "body text")
	assert.Contains(t, got, "38;2;255;255;255", "bold")
	assert.Contains(t, got, "38;2;167;139;250", "heading in the accent")
	assert.Contains(t, got, linkDark, "links and the path")
	assert.Contains(t, ansi.Strip(got), "• one")

	trueColor(t, false)
	renderer, err = markdownRenderer(80)
	require.NoError(t, err)
	got = renderMarkdown(renderer, "Run it **now**.")
	assert.Contains(t, got, "38;2;31;35;40", "light body text")
	assert.Contains(t, got, "38;2;0;0;0", "light bold")
}

// Paths in tool output take the link colour inside the faint gutter.
func TestToolOutputPaths(t *testing.T) {
	trueColor(t, true)
	entry := &toolEntry{name: "list_files", args: map[string]any{"path": "/etc"}, response: map[string]any{"result": "/etc/hosts\n/etc/passwd"}, done: true}
	got := colorLinks(renderTool(entry, 80, false))
	assert.Equal(t, 3, strings.Count(got, linkDark), "the summary and both rows")
}
