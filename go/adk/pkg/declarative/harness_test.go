package declarative

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// TestHarnessToolsAsWorkflowNodes chains the harness tools as tool nodes; CEL
// builds each call's arguments from the previous result and the context.
func TestHarnessToolsAsWorkflowNodes(t *testing.T) {
	workspace := t.TempDir()
	reg := NewRegistry()
	require.NoError(t, reg.RegisterHarnessTools(HarnessConfig{Workspace: workspace}))
	sys := buildDoc(t, reg, head+`
agents:
  - name: files
    agent_class: Workflow
    nodes:
      - id: note
        type: cel
        output: '{"file_path": "notes/" + session.user_id + ".txt", "content": user.content.upperAscii()}'
      - { id: write, type: tool, tool: write_file }
      - { id: read_args, type: cel, output: '{"file_path": "notes/user.txt"}' }
      - { id: read, type: tool, tool: read_file }
      - { id: list_args, type: cel, output: '{"path": "notes"}', state_delta: { content: input.result } }
      - { id: list, type: tool, tool: list_files }
      - { id: shell_args, type: cel, output: '{"command": "wc -c < notes/user.txt | tr -d \" \""}', state_delta: { listing: input.result } }
      - { id: shell, type: tool, tool: bash }
      - id: report
        type: cel
        output: |
          'read=%s listed=%s bytes=%s'.format([
            state.content.contains('HELLO FILES') ? 'ok' : state.content,
            state.listing.contains('user.txt') ? 'ok' : state.listing,
            input.result.trim()])
    edges:
      - { from: START, to: note }
      - { from: note, to: write }
      - { from: write, to: read_args }
      - { from: read_args, to: read }
      - { from: read, to: list_args }
      - { from: list_args, to: list }
      - { from: list, to: shell_args }
      - { from: shell_args, to: shell }
      - { from: shell, to: report }
`)
	svc, id := newSession(t)
	reply := replyText(run(t, sys.Root(), genai.NewContentFromText("hello files", genai.RoleUser), svc, id))
	require.Contains(t, reply, "read=ok listed=ok bytes=11")
	data, err := os.ReadFile(filepath.Join(workspace, "notes", "user.txt"))
	require.NoError(t, err)
	require.Equal(t, "HELLO FILES", string(data))
}

func TestHarnessToolsAreOptIn(t *testing.T) {
	_, err := NewRegistry().Parse([]byte(head + `
agents:
  - name: w
    agent_class: Workflow
    nodes: [{ id: t, type: tool, tool: bash }]
    edges: [{ from: START, to: t }]
`))
	require.ErrorContains(t, err, `"bash"`)

	reg := NewRegistry()
	require.NoError(t, reg.RegisterHarnessTools(HarnessConfig{Workspace: t.TempDir(), DisableShell: true}))
	require.True(t, reg.hasTool("read_file"))
	require.True(t, reg.hasTool("write_file"))
	require.True(t, reg.hasTool("list_files"))
	require.False(t, reg.hasTool("bash"))
	require.False(t, reg.hasTool("edit_file"), "only the basic harness tools are registered")
}
