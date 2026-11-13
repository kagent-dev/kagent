package declarative

import (
	"context"
	"iter"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/agent"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func noopFunc(agent.Context, any) (Result, error) { return Result{}, nil }

func TestRegistryRegistration(t *testing.T) {
	scenarios := []struct {
		name     string
		register func(*Registry) error
		want     string
	}{
		{"a duplicate function name", func(r *Registry) error { return r.RegisterFunction("prefix", noopFunc) }, "already registered"},
		{"a malformed function name", func(r *Registry) error { return r.RegisterFunction("bad name", noopFunc) }, "must match"},
		{"a nil function", func(r *Registry) error { return r.RegisterFunction("nilfn", nil) }, "nil func"},
		{"a nil tool", func(r *Registry) error { return r.RegisterTool(nil) }, "nil tool"},
		{"a duplicate tool", func(r *Registry) error { return r.RegisterTool(r.tools["lookup_customer"]) }, "already registered"},
	}
	for _, sc := range scenarios {
		t.Run("Given a populated registry, when a caller registers "+sc.name+", assert that it is refused", func(t *testing.T) {
			require.ErrorContains(t, sc.register(testRegistry(t)), sc.want)
		})
	}
}

func TestRegistryValidate(t *testing.T) {
	t.Run("Given a registry, when a document names unregistered functions and tools, assert that every reference is reported together", func(t *testing.T) {
		_, err := testRegistry(t).Parse([]byte(docHeader + llmAgent("a", ", model: stub, tools: [lookup_customer, ghost_tool]") +
			"  - name: wf\n    agent_class: Workflow\n    nodes:\n" +
			"      - { id: f, type: function, function: nope }\n" +
			"      - { id: t, type: tool, tool: ghost_tool }\n" +
			"      - { id: l, type: llm, model: stub, instruction: hi, tools: [ghost_tool] }\n" +
			"      - { id: end, type: function, function: prefix }\n" +
			"    edges: [{ from: START, to: f }, { from: f, to: t }, { from: t, to: l }, { from: l, to: end }]\n"))
		require.True(t, IsValidation(err), "got %v", err)
		for _, want := range []string{
			`agents[0].tools[1]: tool "ghost_tool" is not registered`,
			`agents[1].nodes[0].function: function "nope" is not registered`,
			`agents[1].nodes[1].tool: tool "ghost_tool" is not registered`,
			`agents[1].nodes[2].tools[0]: tool "ghost_tool" is not registered`,
		} {
			require.ErrorContains(t, err, want)
		}
		require.NotContains(t, err.Error(), `"lookup_customer"`)
		require.NotContains(t, err.Error(), `"prefix"`)
	})

	t.Run("Given a registry without a model, when an LLM omits model, assert that the missing model is reported", func(t *testing.T) {
		_, err := NewRegistry(nil).Parse([]byte(docHeader + llmAgent("a", "")))
		require.True(t, IsValidation(err), "got %v", err)
		require.ErrorContains(t, err, "agents[0].model: the registry has no model")
	})

	t.Run("Given a registry with a model, when an LLM omits model, assert that the document is valid", func(t *testing.T) {
		_, err := NewRegistry(NewStubModel("main")).Parse([]byte(docHeader + llmAgent("a", "")))
		require.NoError(t, err)
	})
}

// capturingModel records the requests it receives.
type capturingModel struct {
	adkmodel.LLM
	requests chan *adkmodel.LLMRequest
}

func (m capturingModel) GenerateContent(ctx context.Context, req *adkmodel.LLMRequest, stream bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	m.requests <- req
	return m.LLM.GenerateContent(ctx, req, stream)
}

func TestRegistryModel(t *testing.T) {
	t.Run("Given a registry model, when an LlmAgent that omits model runs, assert that it replies through the registry model with its options", func(t *testing.T) {
		model := capturingModel{LLM: NewStubModel("main"), requests: make(chan *adkmodel.LLMRequest, 4)}
		sys := buildDoc(t, NewRegistry(model), docHeader+`  - name: solo
    agent_class: LlmAgent
    instruction: Answer plainly.
    global_instruction: Always be brief.
    output_schema:
      type: object
      properties:
        answer: { type: string }
      required: [answer]
`)
		require.Contains(t, texts(run(t, sys.Root(), "ping")), "[main] ping")
		req := <-model.requests
		instruction := req.Config.SystemInstruction.Parts[0].Text
		require.Contains(t, instruction, "Always be brief.")
		require.Contains(t, instruction, "Answer plainly.")
		require.Equal(t, "application/json", req.Config.ResponseMIMEType)
		require.Contains(t, req.Config.ResponseSchema.Required, "answer")
	})

	t.Run("Given a registry model, when an LLM sets model stub, assert that it replies through the stub model", func(t *testing.T) {
		sys := buildDoc(t, NewRegistry(NewStubModel("main")), docHeader+llmAgent("solo", ", model: stub"))
		require.Contains(t, texts(run(t, sys.Root(), "ping")), "[stub] ping")
	})

	t.Run("Given the stub model, when it receives a conversation, assert that it echoes the last user text", func(t *testing.T) {
		m := NewStubModel("stub-x")
		require.Equal(t, "stub-x", m.Name())
		for resp, err := range m.GenerateContent(t.Context(), &adkmodel.LLMRequest{Contents: []*genai.Content{
			genai.NewContentFromText("first", genai.RoleUser),
			genai.NewContentFromText("reply", genai.RoleModel),
			genai.NewContentFromText("last", genai.RoleUser),
		}}, false) {
			require.NoError(t, err)
			require.Equal(t, "[stub-x] last", resp.Content.Parts[0].Text)
			require.True(t, resp.TurnComplete)
		}
	})
}
