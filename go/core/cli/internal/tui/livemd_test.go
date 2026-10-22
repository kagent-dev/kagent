package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

func TestSplitMarkdown(t *testing.T) {
	for name, tc := range map[string]struct{ text, done string }{
		"a paragraph still arriving":        {"Some words", ""},
		"a finished paragraph":              {"Intro.\n\nNext par", "Intro.\n\n"},
		"a table until its blank line":      {"Intro.\n\n| a | b |\n|---|---|\n| 1 | 2 |\n", "Intro.\n\n"},
		"a blank line inside a code fence":  {"```go\nx := 1\n\ny := 2\n", ""},
		"a closed fence finishes its block": {"```go\nx := 1\n```\nafter", "```go\nx := 1\n```\n"},
		"a fence line still arriving":       {"Intro.\n\n```g", "Intro.\n\n"},
	} {
		t.Run(name, func(t *testing.T) {
			done, tail := splitMarkdown(tc.text)
			assert.Equal(t, tc.done, done)
			assert.Equal(t, tc.text, done+tail)
		})
	}
}

// An unfinished block shows every token as it arrives: table rows as pipes,
// code line by line; finished blocks above it keep their glamour rendering.
func TestLiveMarkdownShowsPartialBlocks(t *testing.T) {
	m := newTestChatModel()
	m.vp.Width, m.vp.Height = 82, 40
	m.agentText = "Intro paragraph.\n\n| Step | What |\n|---|---|\n| 1 | Ask |\n| 2 | Ans"
	m.render()
	got := ansi.Strip(transcript(m))
	assert.Contains(t, got, "Intro paragraph.")
	assert.Contains(t, got, "| 1 | Ask |\n| 2 | Ans", "rows show as they arrive, before the table finishes")
	above := got[:strings.Index(got, "| Step")]

	m.agentText += "wer |\n\n```go\nfunc main() {\n\tfmt.Println(\"hi"
	m.render()
	got = ansi.Strip(transcript(m))
	assert.True(t, strings.HasPrefix(got, above), "lines above the finished block stay put")
	assert.NotContains(t, got, "| 2 | Answer |", "the finished table is glamour's now")
	assert.Contains(t, got, "Answer")
	assert.Contains(t, got, "  func main() {\n      fmt.Println(\"hi", "code shows line by line")
	assert.NotContains(t, got, "```")
}
