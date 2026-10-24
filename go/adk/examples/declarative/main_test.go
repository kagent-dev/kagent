package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	adkagent "google.golang.org/adk/v2/agent"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"

	"github.com/kagent-dev/kagent/go/adk/pkg/declarative"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

// TestAgentsDocumentBuilds guards the example document against drift from the
// registry it is served with.
func TestAgentsDocumentBuilds(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key") // building an OpenAI model needs a key; no request is made
	startCRM(t)
	reg, err := newRegistry(t.TempDir())
	require.NoError(t, err)
	spec, err := reg.Parse(agentsDocument)
	require.NoError(t, err)
	system, err := declarative.Build(t.Context(), spec, reg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, system.Close()) })
	require.Equal(t, "support_triage", system.Root().Name())
}

// startCRM serves the demo CRM MCP server for the test and points the
// document's url_from at it.
func startCRM(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(newCRMServer())
	t.Cleanup(srv.Close)
	t.Setenv(crmURLEnv, srv.URL)
}

func TestLoadDocumentPrefersTheDeliveredWorkflow(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	reg, err := newRegistry(t.TempDir())
	require.NoError(t, err)

	embedded, err := loadDocument(reg, nil)
	require.NoError(t, err)
	require.Equal(t, "support_triage", embedded.Agents[0].Name)

	const document = `{"apiVersion":"adk.declarative/v0.2","kind":"AgentSystem","agents":[{"name":"delivered","agent_class":"LlmAgent","model":"stub","instruction":"hi"}]}`
	sum := sha256.Sum256([]byte(document))
	delivered, err := loadDocument(reg, &configv1.ADKWorkflowConfig{Document: []byte(document), Sha256: hex.EncodeToString(sum[:])})
	require.NoError(t, err)
	require.Equal(t, "delivered", delivered.Agents[0].Name)
}

// TestCELClassification runs every route of the example offline against the
// demo CRM MCP server and a temporary harness workspace: the model resolver is
// replaced by the stub, which echoes "[node] <user text>".
func TestCELClassification(t *testing.T) {
	startCRM(t)
	workspace := t.TempDir()
	reg, err := newRegistry(workspace)
	require.NoError(t, err)
	reg.SetModelResolver(func(_ context.Context, name string) (adkmodel.LLM, error) {
		return declarative.NewStubModel(name), nil
	})
	spec, err := reg.Parse(agentsDocument)
	require.NoError(t, err)
	system, err := declarative.Build(t.Context(), spec, reg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, system.Close()) })

	for _, tc := range []struct {
		name, input string
		want        []string
		state       map[string]any
		files       bool
	}{
		{
			name:  "urgent wins over billing",
			input: "Billing portal is DOWN for c-7",
			want:  []string{"escalated to on-call: BILLING PORTAL IS DOWN FOR C-7", "(ticket saved to tickets/T-"},
			files: true,
		},
		{
			name:  "billing with an extracted customer id",
			input: "Please refund invoice 12 for customer c-42",
			want:  []string{"Customer c-42 is on the pro plan with a balance of 120 ({\"ticket\":\"T-"},
			state: map[string]any{"customer_id": "c-42", "keywords": []any{"refund", "invoice"}},
		},
		{
			name:  "billing without a customer id",
			input: "I was charged twice",
			want:  []string{"Billing requests need a customer id such as c-123 (ticket T-"},
			state: map[string]any{"customer_id": ""},
		},
		{
			name:  "greeting counts keywords from state",
			input: "Hello, my login fails",
			want:  []string{"Hello! Describe a billing issue", "(1 keywords noted)"},
		},
		{
			name:  "question reaches the model with e-mail redacted",
			input: "How do I change jane.doe@example.com on my account?",
			want:  []string{"How do I change <email> on my account?"},
		},
		{
			name:  "unmatched falls back",
			input: "Thanks for the help",
			want:  []string{"I could not classify that request."},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply, state := runExample(t, system, tc.input)
			for _, w := range tc.want {
				require.Contains(t, reply, w)
			}
			for k, v := range tc.state {
				require.Equal(t, v, state[k], "state %q", k)
			}
			if tc.files {
				ticket, err := os.ReadFile(filepath.Join(workspace, "tickets", state["ticket_ref"].(string)+".txt"))
				require.NoError(t, err)
				require.Equal(t, state["escalation"], string(ticket))
			}
		})
	}
}

func runExample(t *testing.T, system *declarative.System, input string) (string, map[string]any) {
	t.Helper()
	sessions := adksession.InMemoryService()
	created, err := sessions.Create(t.Context(), &adksession.CreateRequest{AppName: "example", UserID: "u"})
	require.NoError(t, err)
	r, err := runner.New(runner.Config{AppName: "example", Agent: system.Root(), SessionService: sessions})
	require.NoError(t, err)
	var reply strings.Builder
	for ev, err := range r.Run(t.Context(), "u", created.Session.ID(), genai.NewContentFromText(input, genai.RoleUser), adkagent.RunConfig{}) {
		require.NoError(t, err)
		if ev.Content != nil && !ev.Partial {
			for _, p := range ev.Content.Parts {
				reply.WriteString(p.Text)
			}
		}
	}
	got, err := sessions.Get(t.Context(), &adksession.GetRequest{AppName: "example", UserID: "u", SessionID: created.Session.ID()})
	require.NoError(t, err)
	return reply.String(), maps.Collect(got.Session.State().All())
}
