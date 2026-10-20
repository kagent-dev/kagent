package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolSummary(t *testing.T) {
	tests := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"bash", map[string]any{"command": "git status && git remote -v", "timeout_seconds": 30.0}, "git status && git remote -v"},
		{"read_file", map[string]any{"file_path": "main.go", "offset": 10.0}, "main.go"},
		{"write_file", map[string]any{"file_path": "a/b.txt", "content": "x"}, "a/b.txt"},
		{"edit_file", map[string]any{"file_path": "main.go", "old_string": "a", "new_string": "b"}, "main.go"},
		{"list_files", map[string]any{"path": "src"}, "src"},
		{"grep_file", map[string]any{"pattern": "func main", "path": "cmd"}, "func main in cmd"},
		{"get_pods", map[string]any{"namespace": "default", "labels": map[string]any{"app": "a&b"}}, `labels={"app":"a&b"} namespace=default`},
	}
	for _, tt := range tests {
		t.Run(tt.tool+" "+tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, toolSummary(tt.tool, tt.args))
		})
	}
}

func TestToolOutputUnescapes(t *testing.T) {
	assert.Equal(t, "a && b\nline 2", toolOutput(map[string]any{"output": "a && b\nline 2", "exit_code": 0.0}))
	assert.Equal(t, "     1|package main", toolOutput(map[string]any{"result": "     1|package main"}))
	assert.Equal(t, "{\n  \"pods\": [\n    \"a&b\"\n  ]\n}\n", toolOutput(map[string]any{"pods": []any{"a&b"}}))
}

func TestRenderTool(t *testing.T) {
	bash := func(output string, code float64) *toolEntry {
		return &toolEntry{name: "bash", args: map[string]any{"command": "ls"}, response: map[string]any{"output": output, "exit_code": code}, done: true}
	}
	t.Run("collapsed shows the first lines and how many more", func(t *testing.T) {
		got := ansi.Strip(renderTool(bash("1\n2\n3\n4\n5\n", 0), 80, false))
		assert.Equal(t, "● bash  ls\n  │ 1\n  │ 2\n  │ 3\n  │ +2 lines", got)
	})
	t.Run("expanded shows all of it", func(t *testing.T) {
		got := ansi.Strip(renderTool(bash("1\n2\n3\n4\n5\n", 0), 80, true))
		assert.Equal(t, "● bash  ls\n  │ 1\n  │ 2\n  │ 3\n  │ 4\n  │ 5", got)
	})
	t.Run("a failure says its exit code", func(t *testing.T) {
		got := ansi.Strip(renderTool(bash("boom", 2), 80, false))
		assert.Equal(t, "✗ bash  ls\n  │ boom\n  │ exit 2", got)
	})
	t.Run("a built-in tool's error text is a failure", func(t *testing.T) {
		entry := &toolEntry{name: "read_file", args: map[string]any{"file_path": "x"}, response: map[string]any{"result": "Error reading file x: not found"}, done: true}
		assert.Equal(t, "✗ read_file  x\n  │ Error reading file x: not found", ansi.Strip(renderTool(entry, 80, false)))
	})
	t.Run("running", func(t *testing.T) {
		assert.Equal(t, "○ read_file  main.go", ansi.Strip(renderTool(&toolEntry{name: "read_file", args: map[string]any{"file_path": "main.go"}}, 80, false)))
	})
	t.Run("denied", func(t *testing.T) {
		entry := &toolEntry{name: "write_file", args: map[string]any{"file_path": "/etc/x"}, response: map[string]any{"error": "denied: write /etc/x"}, done: true}
		assert.Equal(t, "⊘ write_file  /etc/x\n  │ write /etc/x", ansi.Strip(renderTool(entry, 80, false)))
	})
	t.Run("an edit counts its lines and hides the wrote note", func(t *testing.T) {
		entry := &toolEntry{
			name: "edit_file", args: map[string]any{"file_path": "main.go", "old_string": "a\nb", "new_string": "c\nd\ne\n"},
			response: map[string]any{"result": "Successfully edited file: /data/workspace/main.go"}, done: true,
		}
		assert.Equal(t, "● edit_file  main.go  +3 -2", ansi.Strip(renderTool(entry, 80, false)))
	})
	t.Run("read numbers pad only to the widest shown", func(t *testing.T) {
		entry := &toolEntry{name: "read_file", args: map[string]any{"file_path": "a.py"}, response: map[string]any{"result": "     9|x = 1\n    10|y = 2\n[read limited to 2 lines; continue with offset=11]"}, done: true}
		assert.Equal(t, "● read_file  a.py\n  │  9  x = 1\n  │ 10  y = 2\n  │ [read limited to 2 lines; continue with offset=11]", ansi.Strip(renderTool(entry, 80, false)))
	})
	t.Run("long rows fit the width", func(t *testing.T) {
		got := renderTool(bash(strings.Repeat("x", 100), 0), 40, false)
		for line := range strings.SplitSeq(got, "\n") {
			assert.LessOrEqual(t, ansi.StringWidth(line), 40)
		}
	})
}

func TestChatModelTogglesToolOutput(t *testing.T) {
	m := newTestChatModel()
	m.vp.Width = 82
	m.appendBlock(&block{tool: &toolEntry{name: "bash", args: map[string]any{"command": "seq 5"}, response: map[string]any{"output": "1\n2\n3\n4\n5"}, done: true}})
	assert.Contains(t, transcript(m), "+2 lines")
	m.toggleTools()
	assert.NotContains(t, transcript(m), "+2 lines")
	assert.Contains(t, transcript(m), "│ 5")
}

func TestRenderMarkdown(t *testing.T) {
	const doc = "## Summary\n\n| File | Change |\n|---|---|\n| `main.go` | prints **hello** |\n\n- one\n- two\n\n```go\nfunc main() {}\n```\n\nSee [docs](https://kagent.dev/docs)."
	for _, width := range []int{40, 100} {
		renderer, err := markdownRenderer(width)
		require.NoError(t, err)
		got := renderMarkdown(renderer, doc)
		plain := ansi.Strip(got)
		assert.NotContains(t, plain, "|---|", "tables render")
		assert.NotContains(t, plain, "```", "code fences render")
		assert.NotContains(t, plain, "**", "bold renders")
		assert.Contains(t, plain, "Summary")
		assert.Contains(t, plain, "func main() {}")
		for line := range strings.SplitSeq(got, "\n") {
			assert.LessOrEqual(t, ansi.StringWidth(line), width, "fits %d columns", width)
		}
	}
}

func TestRenderUser(t *testing.T) {
	assert.Equal(t, "❯ hi\n  there", ansi.Strip(renderUser("hi\nthere", 80)))
	assert.Equal(t, "❯ one two\n  three", ansi.Strip(renderUser("one two three", 11)), "wrapped lines stay under the glyph")
}

// Markdown sits one blank line from its neighbours, like every other block.
func TestRenderMarkdownTrimsBlankEdges(t *testing.T) {
	renderer, err := markdownRenderer(60)
	require.NoError(t, err)
	got := renderMarkdown(renderer, "- one\n- two")
	assert.Equal(t, "• one\n• two", ansi.Strip(got))
}

// Agent markdown wraps to the pane, and its links stay Cmd+clickable.
func TestChatModelRendersMarkdownLinks(t *testing.T) {
	m := newTestChatModel()
	m.vp.Width, m.vp.Height = 62, 40
	m.agentText = "See [docs](https://kagent.dev/docs) for **more**."
	m.commitAgentText()
	m.render()
	view := m.vp.View()
	assert.Contains(t, view, ansi.SetHyperlink("https://kagent.dev/docs")+"https://kagent.dev/docs"+ansi.ResetHyperlink())
	assert.NotContains(t, ansi.Strip(view), "**")
}

// Angle brackets read as text, not HTML, so glamour keeps them; code shows them unescaped.
func TestRenderMarkdownKeepsAngleBrackets(t *testing.T) {
	renderer, err := markdownRenderer(80)
	require.NoError(t, err)
	for _, tc := range []struct{ name, doc, want string }{
		{"plain text", "Run kubectl logs <pod-name> -f.", "kubectl logs <pod-name> -f."},
		{"inline code", "Run `kubectl logs <pod-name>` now.", "kubectl logs <pod-name>"},
		{"double backtick code", "Run ``a `<b>` c`` now.", "a `<b>` c"},
		{"fenced block", "```sh\nkubectl logs <pod-name>\n```", "kubectl logs <pod-name>"},
		{"tilde inside backtick fence", "```\na\n~~~\nb <y>\n```\nafter <z>", "after <z>"},
		{"short fence inside long fence", "````\n```\nin <q>\n````\nout <w>", "out <w>"},
		{"already escaped", `Use \<tag> here.`, "Use <tag> here."},
		{"link", "See [docs](https://kagent.dev/docs) for <name>.", "docs https://kagent.dev/docs for <name>."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ansi.Strip(renderMarkdown(renderer, tc.doc))
			assert.Contains(t, got, tc.want)
			assert.NotContains(t, got, `\`)
		})
	}
}
