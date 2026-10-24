package translator

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const workflowDocument = `
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: root
    agent_class: Workflow
    entry: only
    nodes:
      - { id: only, type: function, function: handle }
`

func TestValidateWorkflow(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr []string
	}{
		{name: "yaml document", raw: workflowDocument},
		{
			name: "json document",
			raw:  `{"apiVersion":"adk.declarative/v0.2","kind":"AgentSystem","agents":[{"name":"root","agent_class":"Workflow","entry":"only","nodes":[{"id":"only","type":"function","function":"handle"}]}]}`,
		},
		{name: "empty", raw: "", wantErr: []string{"document is empty"}},
		{name: "not a document", raw: "- just\n- a list", wantErr: []string{"failed to decode AgentSystem document"}},
		{
			name:    "unknown field",
			raw:     strings.Replace(workflowDocument, "entry: only", "entry: only\n    colour: red", 1),
			wantErr: []string{"failed to decode AgentSystem document", `"colour"`},
		},
		{
			name: "all issues are reported with field paths",
			raw: `
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: root
    agent_class: Workflow
    nodes:
      - { id: a, type: function }
      - { id: b, type: tool, tool: lookup }
    edges:
      - { from: START, to: a }
      - { from: a, to: ghost }
      - { from: a, to: b }
`,
			wantErr: []string{
				"agents[0].nodes[0].function: required for node type function",
				`agents[0].edges[1].to: unknown node "ghost"`,
				"agents[0].nodes[1]: terminal tool node emits structured output",
			},
		},
		{name: "too large", raw: workflowDocument + "# " + strings.Repeat("x", maxWorkflowBytes), wantErr: []string{"document exceeds"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			canonical, err := validateWorkflow([]byte(tt.raw))
			if len(tt.wantErr) > 0 {
				require.Error(t, err)
				for _, want := range tt.wantErr {
					require.ErrorContains(t, err, want)
				}
				return
			}
			require.NoError(t, err)
			require.JSONEq(t, `{"apiVersion":"adk.declarative/v0.2","kind":"AgentSystem","agents":[{"name":"root","agent_class":"Workflow","nodes":[{"id":"only","type":"function","function":"handle"}],"entry":"only"}]}`, string(canonical))
		})
	}
}

func TestValidateWorkflowCanonicalFormIsStable(t *testing.T) {
	yamlForm, err := validateWorkflow([]byte(workflowDocument))
	require.NoError(t, err)
	// The canonical form is itself a valid document with the same canonical form.
	again, err := validateWorkflow(yamlForm)
	require.NoError(t, err)
	require.Equal(t, string(yamlForm), string(again))
}

func TestValidateWorkflowBoundsCanonicalSize(t *testing.T) {
	// JSON encoding escapes "<" as \u003c, so the canonical form is larger than
	// the input. The proto field that carries it has the same bound.
	raw := `
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - { name: a, agent_class: LlmAgent, model: m, instruction: "` + strings.Repeat("<", 20000) + `" }
`
	require.Less(t, len(raw), maxWorkflowBytes)
	_, err := validateWorkflow([]byte(raw))
	require.ErrorContains(t, err, "canonical document exceeds")
}
