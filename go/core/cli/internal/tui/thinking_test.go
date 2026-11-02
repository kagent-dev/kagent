package tui

import (
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"

	kagenta2a "github.com/kagent-dev/kagent/go/api/a2a"
)

func thoughtPart(text string) *a2atype.Part {
	part := a2atype.NewTextPart(text)
	part.SetMeta(kagenta2a.ThoughtMetadataKey, true)
	return part
}

// Reasoning summaries stream into one faint thought block, apart from the answer,
// which closes when the model acts; ctrl+x shows the summary.
func TestChatModelRendersThoughts(t *testing.T) {
	m := newTestChatModel()
	m.vp.Width, m.vp.Height = 82, 40
	m.setWorkingTime(time.Time{})
	first := a2atype.NewArtifactEvent(reqCtx(), thoughtPart("Plan the"))
	m.appendEvent(first)
	m.appendEvent(a2atype.NewArtifactUpdateEvent(reqCtx(), first.Artifact.ID, thoughtPart(" fix.")))
	assert.Equal(t, "✻ Thinking…", ansi.Strip(transcript(m)))
	assert.True(t, strings.HasPrefix(m.statusText, "Thinking… "), m.statusText)

	m.appendEvent(a2atype.NewArtifactEvent(reqCtx(),
		dataPart("function_call", "bash", map[string]any{"args": map[string]any{"command": "ls"}})))
	assert.True(t, strings.HasPrefix(m.statusText, "Running bash… "), m.statusText)
	m.appendEvent(a2atype.NewArtifactUpdateEvent(reqCtx(), first.Artifact.ID, a2atype.NewTextPart("Done.")))

	got := ansi.Strip(transcript(m))
	assert.Contains(t, got, "✻ Thought\n")
	assert.NotContains(t, got, "Plan the fix", "a collapsed thought hides its summary")
	assert.Contains(t, got, "Done.")

	m.toggleTools()
	assert.Contains(t, ansi.Strip(transcript(m)), "✻ Thought\n  │ Plan the fix.")
}

// A step title loses its markers, and the summary is sanitized like other model text.
func TestRenderThoughtExpanded(t *testing.T) {
	thought := &thoughtEntry{text: "**Checking the math**\n\nIt \x1b[31madds\x1b[0m up.", start: time.Now(), end: time.Now()}
	assert.Equal(t, "✻ Thought\n  │ Checking the math\n  │ \n  │ It [31madds[0m up.", ansi.Strip(renderThought(thought, 80, true)))
}

// The status line names what the turn is doing, in the web UI's words.
func TestChatModelActivity(t *testing.T) {
	m := newTestChatModel()
	m.setWorkingTime(time.Time{})
	label, _ := m.activity()
	assert.Equal(t, "Sent", label, "nothing has come back yet")
	m.appendEvent(a2atype.NewStatusUpdateEvent(reqCtx(), a2atype.TaskStateWorking, nil))
	label, _ = m.activity()
	assert.Equal(t, "Working…", label)
	m.appendEvent(a2atype.NewArtifactEvent(reqCtx(), a2atype.NewTextPart("Hel")))
	label, _ = m.activity()
	assert.Equal(t, "Answering…", label)
}
