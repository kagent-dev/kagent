package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/kagent-dev/kagent/go/core/cli/internal/tui/theme"
)

// splitMarkdown cuts streamed markdown after its last finished block: a blank
// line outside a code fence, or a closing fence. Glamour renders the finished
// part; the rest is still arriving and shows as plain text.
func splitMarkdown(text string) (done, tail string) {
	cut, inFence, offset := 0, false, 0
	for line := range strings.SplitAfterSeq(text, "\n") {
		offset += len(line)
		if !strings.HasSuffix(line, "\n") {
			break // the line is still arriving
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~"):
			if inFence {
				cut = offset
			}
			inFence = !inFence
		case trimmed == "" && !inFence:
			cut = offset
		}
	}
	return text[:cut], text[cut:]
}

// renderTail draws an unfinished markdown block as lightly styled text, so every
// token shows at once: code lines in the code colour, table rows as their pipes.
func renderTail(tail string) string {
	code, text := lipgloss.NewStyle().Foreground(theme.ColorCodeBlock), lipgloss.NewStyle().Foreground(theme.ColorText)
	var rows []string
	inFence := false
	for line := range strings.SplitSeq(strings.TrimRight(sanitize(tail), "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~"):
			inFence = !inFence // glamour hides the fences too
		case inFence:
			rows = append(rows, "  "+code.Render(line))
		case strings.HasPrefix(trimmed, "#"):
			rows = append(rows, theme.HeadingStyle().Render(strings.TrimSpace(strings.TrimLeft(trimmed, "#"))))
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			rows = append(rows, text.Render(strings.Replace(line, trimmed[:2], "• ", 1)))
		default:
			rows = append(rows, text.Render(line))
		}
	}
	return strings.Join(rows, "\n")
}

// renderLive renders the reply still streaming: glamour for its finished blocks,
// rendered again only when another block finishes, and plain text for the rest.
func (m *chatModel) renderLive() string {
	done, tail := splitMarkdown(m.agentText)
	if done != m.liveDone || m.liveOut == "" && done != "" {
		m.liveDone, m.liveOut = done, renderMarkdown(m.md, done)
	}
	switch {
	case done == "":
		return renderTail(tail)
	case strings.TrimSpace(tail) == "":
		return m.liveOut
	}
	return m.liveOut + "\n\n" + renderTail(tail)
}
