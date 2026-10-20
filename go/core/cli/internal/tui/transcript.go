package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/glamour"
	glamouransi "github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/kagent-dev/kagent/go/core/cli/internal/tui/theme"
)

// collapsedOutputLines is how much of a tool's output shows before ctrl+x expands it.
const collapsedOutputLines = 3

// block is one transcript entry: styled text, agent markdown, a thought, or a
// tool call with its result. Markdown and tool calls render at the viewport's width.
type block struct {
	text     string
	markdown bool
	// user marks the user's own message, which wraps under its prompt glyph.
	user    bool
	tool    *toolEntry
	thought *thoughtEntry
	// expanded shows a tool's full output or a thought's summary.
	expanded bool

	// cache is text rendered at cacheWidth, so only the live block re-renders per delta.
	cache      string
	cacheWidth int
}

// toolEntry is a call, filled in by its result when that arrives.
type toolEntry struct {
	started  time.Time
	name     string
	args     map[string]any
	response any
	done     bool
}

type toolStatus int

const (
	toolRunning toolStatus = iota
	toolOK
	toolError
	toolDenied
)

func (t *toolEntry) status() toolStatus {
	if !t.done {
		return toolRunning
	}
	result, _ := t.response.(map[string]any)
	if msg, ok := result["error"].(string); ok && (strings.HasPrefix(msg, "denied") || strings.Contains(msg, "rejected")) {
		return toolDenied
	}
	// The built-in tools report failures as text that starts with "Error".
	if output, _ := result["result"].(string); strings.HasPrefix(output, "Error") {
		return toolError
	}
	if result["error"] != nil || exitCode(result) != 0 {
		return toolError
	}
	return toolOK
}

func exitCode(result map[string]any) int {
	code, _ := result["exit_code"].(float64)
	return int(code)
}

// sanitize drops control characters other than newline and tab, including
// ESC, the C1 range and stray bytes, so text from tools, models and users
// cannot hide text, move the cursor or forge a link once drawn.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f, r == utf8.RuneError:
			return -1
		}
		return r
	}, s)
}

// toolSummary is what a call does, in the words its tool takes: the command, the path, the pattern.
func toolSummary(name string, args map[string]any) string {
	return sanitize(rawToolSummary(name, args))
}

func rawToolSummary(name string, args map[string]any) string {
	str := func(key string) string { s, _ := args[key].(string); return s }
	switch name {
	case "bash":
		return str("command")
	case "read_file", "write_file", "edit_file":
		return str("file_path")
	case "list_files":
		return str("path")
	case "grep_file":
		if path := str("path"); path != "" {
			return str("pattern") + " in " + path
		}
		return str("pattern")
	}
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		value, ok := args[key].(string)
		if !ok {
			value = compactJSON(args[key])
		}
		pairs = append(pairs, key+"="+value)
	}
	return strings.Join(pairs, " ")
}

// lineDelta counts an edit's or write's lines, as +added and -removed.
func lineDelta(name string, args map[string]any) (added, removed int) {
	count := func(key string) int {
		s, _ := args[key].(string)
		if s == "" {
			return 0
		}
		return strings.Count(strings.TrimSuffix(s, "\n"), "\n") + 1
	}
	switch name {
	case "edit_file":
		return count("new_string"), count("old_string")
	case "write_file":
		return count("content"), 0
	}
	return 0, 0
}

// toolOutput is a result's text with real newlines: the output, result or error a tool returned, else its JSON.
func toolOutput(response any) string {
	return sanitize(rawToolOutput(response))
}

func rawToolOutput(response any) string {
	switch value := response.(type) {
	case nil:
		return ""
	case string:
		return value
	case map[string]any:
		for _, key := range []string{"output", "result", "error"} {
			if s, ok := value[key].(string); ok {
				return s
			}
		}
	}
	encoded := &bytes.Buffer{}
	encoder := json.NewEncoder(encoded)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(response); err != nil {
		return fmt.Sprintf("%v", response)
	}
	return encoded.String()
}

// compactJSON writes a value on one line, without escaping &, < and >.
func compactJSON(value any) string {
	encoded := &bytes.Buffer{}
	encoder := json.NewEncoder(encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return fmt.Sprintf("%v", value)
	}
	return strings.TrimSuffix(encoded.String(), "\n")
}

// renderTool draws a call as one line, glyph then tool then summary, with its
// output under a gutter: the first lines when collapsed, all of it expanded.
// Each status has its own glyph, so it reads without color too.
func renderTool(t *toolEntry, width int, expanded bool) string {
	status := t.status()
	glyph := map[toolStatus]string{
		toolRunning: theme.DimStyle().Render("○"),
		toolOK:      theme.ReadyStyle().Render("●"),
		toolError:   theme.ErrorStyle().Render("✗"),
		toolDenied:  theme.AttentionStyle().Render("⊘"),
	}[status]
	line := glyph + " " + theme.HeadingStyle().Render(sanitize(t.name))
	if summary := toolSummary(t.name, t.args); summary != "" {
		line += "  " + summary
	}
	if added, removed := lineDelta(t.name, t.args); added+removed > 0 {
		line += "  " + theme.ReadyStyle().Render(fmt.Sprintf("+%d", added))
		if removed > 0 {
			line += " " + theme.ErrorStyle().Render(fmt.Sprintf("-%d", removed))
		}
	}
	lines := []string{ansi.Truncate(firstLine(line, expanded), width, "…")}
	if expanded {
		lines = strings.Split(ansi.Wrap(line, width, ""), "\n")
	}

	output := strings.TrimRight(toolOutput(t.response), "\n")
	if status == toolDenied {
		output = strings.TrimSpace(strings.TrimPrefix(output, "denied:"))
	}
	// A write or edit says only where it wrote, which the call line already shows.
	if status == toolOK && (t.name == "write_file" || t.name == "edit_file") {
		output = ""
	}
	style := theme.FaintStyle()
	if status == toolError {
		style = theme.ErrorStyle().Bold(false)
	}
	gutter := theme.FaintStyle().Render("  │ ")
	inner := max(width-4, 8)
	var body []string
	if output != "" {
		body = strings.Split(output, "\n")
	}
	hidden := 0
	if !expanded && len(body) > collapsedOutputLines {
		hidden, body = len(body)-collapsedOutputLines, body[:collapsedOutputLines]
	}
	if t.name == "read_file" {
		body = numberLines(body)
	}
	for _, row := range body {
		row = strings.ReplaceAll(row, "\t", "    ")
		if !expanded {
			lines = append(lines, gutter+style.Render(ansi.Truncate(row, inner, "…")))
			continue
		}
		for wrapped := range strings.SplitSeq(ansi.Wrap(row, inner, ""), "\n") {
			lines = append(lines, gutter+style.Render(wrapped))
		}
	}
	var notes []string
	if hidden > 0 {
		notes = append(notes, fmt.Sprintf("+%d lines", hidden))
	}
	if result, _ := t.response.(map[string]any); exitCode(result) != 0 {
		notes = append(notes, fmt.Sprintf("exit %d", exitCode(result)))
	}
	if len(notes) > 0 {
		lines = append(lines, gutter+style.Render(strings.Join(notes, " · ")))
	}
	return strings.Join(lines, "\n")
}

// thoughtEntry is the model's reasoning summary for one stretch of thinking;
// end is zero while the model is still thinking.
type thoughtEntry struct {
	text       string
	start, end time.Time
}

// renderThought is one faint line; expanded, the summary follows in italics,
// sanitized like any other model text.
func renderThought(t *thoughtEntry, width int, expanded bool) string {
	line := "✻ Thought"
	if t.end.IsZero() {
		line = "✻ Thinking…"
	}
	rows := []string{theme.FaintStyle().Render(line)}
	if !expanded {
		return rows[0]
	}
	text := theme.FaintStyle().Italic(true)
	for row := range strings.SplitSeq(ansi.Wrap(strings.TrimSpace(sanitize(t.text)), max(width-4, 8), ""), "\n") {
		// Summaries title each step **like this**; the title reads bold without its markers.
		style := text
		if title, ok := strings.CutPrefix(row, "**"); ok && strings.HasSuffix(title, "**") && len(title) > 2 {
			row, style = strings.TrimSuffix(title, "**"), text.Bold(true)
		}
		rows = append(rows, theme.FaintStyle().Render("  │ ")+style.Render(row))
	}
	return strings.Join(rows, "\n")
}

// readLine is one row of read_file's output: a padded line number, a bar, the text.
var readLine = regexp.MustCompile(`^\s*(\d+)\|(.*)$`)

// numberLines swaps read_file's six-wide line numbers for ones padded only to
// the widest shown. Rows that are not numbered lines, such as a limit note, stay as they are.
func numberLines(rows []string) []string {
	digits := 0
	for _, row := range rows {
		if match := readLine.FindStringSubmatch(row); match != nil {
			digits = max(digits, len(match[1]))
		}
	}
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = row
		if match := readLine.FindStringSubmatch(row); match != nil {
			out[i] = fmt.Sprintf("%*s  %s", digits, match[1], match[2])
		}
	}
	return out
}

// firstLine keeps a multi-line command to its first line, marking the rest.
func firstLine(s string, expanded bool) string {
	if head, _, cut := strings.Cut(s, "\n"); cut && !expanded {
		return head + " …"
	}
	return s
}

// renderUser marks the user's own message with a prompt glyph in the accent
// color, wrapping the text to width and indenting every further line under it.
func renderUser(text string, width int) string {
	rows := strings.Split(ansi.Wrap(text, max(width-2, 8), ""), "\n")
	for i, row := range rows {
		rows[i] = theme.HeadingStyle().Render(row) // bold, so the prompt stands apart from replies
	}
	return theme.UserStyle().Render("❯") + " " + strings.Join(rows, "\n  ")
}

// markdownStyle is glamour's style for the terminal's background, recolored
// to the theme, with the document margin removed since the pane has its own.
func markdownStyle() glamouransi.StyleConfig {
	style, pick := styles.LightStyleConfig, func(c lipgloss.AdaptiveColor) *string { return &c.Light }
	if lipgloss.HasDarkBackground() {
		style, pick = styles.DarkStyleConfig, func(c lipgloss.AdaptiveColor) *string { return &c.Dark }
	}
	zero := uint(0)
	style.Document = glamouransi.StyleBlock{Margin: &zero}
	style.Document.Color = pick(theme.ColorText)
	style.Strong.Color = pick(theme.ColorStrong)
	style.Heading.Color = pick(theme.ColorAccent)
	style.H1 = glamouransi.StyleBlock{}
	style.H2, style.H3, style.H4, style.H5 = style.H1, style.H1, style.H1, style.H1
	style.H6 = glamouransi.StyleBlock{StylePrimitive: glamouransi.StylePrimitive{Bold: new(false)}}
	style.Link = glamouransi.StylePrimitive{Color: pick(theme.ColorLink), Underline: new(true)}
	style.LinkText = style.Link
	style.Code.Color = pick(theme.ColorAttention)
	style.CodeBlock.Color = pick(theme.ColorCodeBlock)
	return style
}

// markdownRenderer is glamour at one width; the chat keeps one until the width changes.
func markdownRenderer(width int) (*glamour.TermRenderer, error) {
	return glamour.NewTermRenderer(
		glamour.WithStyles(markdownStyle()),
		glamour.WithWordWrap(width),
		glamour.WithColorProfile(lipgloss.ColorProfile()),
	)
}

// escapeHTML backslash-escapes "<" outside code, since glamour strips raw HTML
// and would drop text like <pod-name> that the streaming tail still shows.
func escapeHTML(text string) string {
	var b strings.Builder
	fence := "" // the open fence's marker run; it closes on the same char, at least as long
	for line := range strings.SplitAfterSeq(text, "\n") {
		rest := strings.TrimLeft(line, " ")
		if len(line)-len(rest) < 4 && rest != "" && (rest[0] == '`' || rest[0] == '~') {
			info := strings.TrimLeft(rest, rest[:1])
			run := rest[:len(rest)-len(info)]
			switch {
			case len(run) < 3:
			case fence == "" && (run[0] != '`' || !strings.Contains(info, "`")):
				fence = run
				b.WriteString(line)
				continue
			case fence != "" && run[0] == fence[0] && len(run) >= len(fence) && strings.TrimSpace(info) == "":
				fence = ""
				b.WriteString(line)
				continue
			}
		}
		if fence != "" {
			b.WriteString(line)
			continue
		}
		for i := 0; i < len(line); {
			switch {
			case line[i] == '`':
				// A code span runs to the next backtick run of the same length; unclosed ticks are literal.
				ticks := len(line[i:]) - len(strings.TrimLeft(line[i:], "`"))
				end := i + ticks
				for j := end; j < len(line); {
					k := strings.IndexByte(line[j:], '`')
					if k < 0 {
						break
					}
					k += j
					run := len(line[k:]) - len(strings.TrimLeft(line[k:], "`"))
					if run == ticks {
						end = k + run
						break
					}
					j = k + run
				}
				b.WriteString(line[i:end])
				i = end
			case line[i] == '<' && (i == 0 || line[i-1] != '\\'):
				b.WriteString(`\<`)
				i++
			default:
				b.WriteByte(line[i])
				i++
			}
		}
	}
	return b.String()
}

// renderMarkdown renders agent text, falling back to the raw text if glamour fails.
func renderMarkdown(renderer *glamour.TermRenderer, text string) string {
	text = sanitize(text)
	if renderer == nil {
		return text
	}
	out, err := renderer.Render(escapeHTML(text))
	if err != nil {
		return text
	}
	// glamour pads every row to the wrap width and frames the document in blank lines.
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	blank := func(line string) bool { return strings.TrimSpace(ansi.Strip(line)) == "" }
	for len(lines) > 0 && blank(lines[0]) {
		lines = lines[1:]
	}
	for len(lines) > 0 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// escapeSeq is one terminal escape sequence: styling, or an OSC such as a hyperlink.
var escapeSeq = regexp.MustCompile("\x1b(?:\\[[0-9;:?]*[A-Za-z]|\\][^\x07\x1b]*(?:\x07|\x1b\\\\))")

// linkish is a web URL, or a file path that starts /, ~/, ./ or ../.
var linkish = regexp.MustCompile("https?://[^\\s<>\"'`\x1b]+|(?:~|\\.{1,2})?/[\\w.\\-]+(?:/[\\w.\\-]*)*")

// proseColors are the foreground escapes of text that links may sit in: body,
// bold, faint and error text, in the terminal's colour profile.
func proseColors() []string {
	var out []string
	for _, c := range []lipgloss.AdaptiveColor{theme.ColorText, theme.ColorStrong, theme.ColorFaint, theme.ColorError} {
		if start, _, _ := strings.Cut(lipgloss.NewStyle().Foreground(c).Render("x"), "x"); start != "" {
			out = append(out, strings.TrimSuffix(strings.TrimPrefix(start, "\x1b["), "m"))
		}
	}
	return out
}

// colorLinks draws the URLs and file paths in styled text in the link colour,
// then restores the styling in force.
// Paths are only coloured: a click opens web links alone.
func colorLinks(s string) string {
	link, _, _ := strings.Cut(theme.LinkStyle().Render("x"), "x")
	if link == "" {
		return s // no colours in this terminal
	}
	var out strings.Builder
	active := ""
	for s != "" {
		loc := escapeSeq.FindStringIndex(s)
		text := s
		if loc != nil {
			text = s[:loc[0]]
		}
		out.WriteString(colorRun(text, link, active))
		if loc == nil {
			break
		}
		seq := s[loc[0]:loc[1]]
		switch {
		case seq == "\x1b[0m" || seq == "\x1b[m":
			active = ""
		case strings.HasPrefix(seq, "\x1b[") && strings.HasSuffix(seq, "m"):
			active += seq
		}
		out.WriteString(seq)
		s = s[loc[1]:]
	}
	return out.String()
}

// colorRun colours the links that start a word in one run of text. Only plain
// prose and tool output qualify: code, headings and highlighted text keep their colours.
func colorRun(text, link, active string) string {
	if active != "" && !slices.ContainsFunc(proseColors(), func(fg string) bool { return strings.Contains(active, fg) }) || strings.Contains(active, "48;") {
		return text
	}
	var out strings.Builder
	last := 0
	for _, loc := range linkish.FindAllStringIndex(text, -1) {
		if loc[0] > 0 && !strings.ContainsRune(" \t([\"'`", rune(text[loc[0]-1])) {
			continue
		}
		end := loc[0] + len(strings.TrimRight(text[loc[0]:loc[1]], ".,;:!?)]}"))
		out.WriteString(text[last:loc[0]] + link + text[loc[0]:end] + "\x1b[0m" + active)
		last = end
	}
	return out.String() + text[last:]
}
