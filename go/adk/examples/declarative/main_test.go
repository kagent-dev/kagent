package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/require"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"

	"github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	"github.com/kagent-dev/kagent/go/adk/pkg/declarative"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

func delivered(document string) *configv1.ADKWorkflowConfig {
	sum := sha256.Sum256([]byte(document))
	return &configv1.ADKWorkflowConfig{Document: []byte(document), Sha256: hex.EncodeToString(sum[:])}
}

func TestLoadDocumentRequiresTheDeliveredWorkflow(t *testing.T) {
	reg, err := newRegistry(nil, t.TempDir())
	require.NoError(t, err)

	_, err = loadDocument(reg, nil)
	require.ErrorContains(t, err, "spec.workflowFrom")

	spec, err := loadDocument(reg, delivered(`{"apiVersion":"adk.declarative/v0.2","kind":"AgentSystem","agents":[{"name":"delivered","agent_class":"LlmAgent","model":"stub","instruction":"hi"}]}`))
	require.NoError(t, err)
	require.Equal(t, "delivered", spec.Agents[0].Name)
}

func TestModelResolverMapsEveryNameToTheModelConfig(t *testing.T) {
	model := &configv1.ADKModelConfig{}
	created := 0
	resolve := modelResolver(model, func(_ context.Context, got *configv1.ADKModelConfig) (adkmodel.LLM, error) {
		require.Same(t, model, got)
		created++
		return declarative.NewStubModel("configured"), nil
	})

	first, err := resolve(t.Context(), "reviewer")
	require.NoError(t, err)
	second, err := resolve(t.Context(), "gpt-4o")
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, 1, created)
}

func TestModelResolverWithoutModelConfigUsesTheNameAsAnOpenAIModel(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key") // creating the client needs a key; no request is made
	llm, err := modelResolver(nil, nil)(t.Context(), "gpt-4o-mini")
	require.NoError(t, err)
	require.Equal(t, "gpt-4o-mini", llm.Name())
}

func TestAppName(t *testing.T) {
	require.Equal(t, "kagent__NS__writer_critics", appName("kagent", "writer-critics", "root"))
	require.Equal(t, "root", appName("", "writer-critics", "root"))
}

// exampleDocuments are the workflows this runtime is deployed with.
func exampleDocuments(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob("../workflow/*/agent-system.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	return paths
}

// TestExampleWorkflowsRunAndNameEveryReply runs each example document offline
// through the executor the runtime serves with: every model name resolves to
// the template's model (here the stub), and every reply reaches the chat as its
// own artifact naming the node that wrote it.
func TestExampleWorkflowsRunAndNameEveryReply(t *testing.T) {
	for _, path := range exampleDocuments(t) {
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			document, err := os.ReadFile(path)
			require.NoError(t, err)
			reg, err := newRegistry(&configv1.ADKModelConfig{}, t.TempDir())
			require.NoError(t, err)
			reg.SetModelResolver(modelResolver(&configv1.ADKModelConfig{}, func(context.Context, *configv1.ADKModelConfig) (adkmodel.LLM, error) {
				return declarative.NewStubModel("template-model"), nil
			}))
			spec, err := loadDocument(reg, delivered(string(document)))
			require.NoError(t, err)
			system, err := declarative.Build(t.Context(), spec, reg)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, system.Close()) })

			root := system.Root()
			sessions := adksession.InMemoryService()
			executor, err := a2a.NewKAgentExecutor(a2a.KAgentExecutorConfig{
				RunnerConfig:   runner.Config{AppName: "test", Agent: root, SessionService: sessions},
				SessionService: sessions,
				AppName:        "test",
				Logger:         slog.New(slog.DiscardHandler),
			})
			require.NoError(t, err)

			reqCtx := &a2asrv.ExecutorContext{
				TaskID:    "task-1",
				ContextID: "context-1",
				Message:   a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("why is the sky blue")),
			}
			ids := map[a2atype.ArtifactID]bool{}
			authors := map[string]bool{}
			for event, err := range executor.Execute(t.Context(), reqCtx) {
				require.NoError(t, err)
				update, ok := event.(*a2atype.TaskArtifactUpdateEvent)
				if !ok || update.Artifact == nil {
					continue
				}
				author, _ := update.Artifact.Metadata[apia2a.AuthorMetadataKey].(string)
				require.NotEmpty(t, author, "artifact %s has no author", update.Artifact.ID)
				require.False(t, ids[update.Artifact.ID], "reply appended to artifact %s", update.Artifact.ID)
				ids[update.Artifact.ID] = true
				authors[author] = true
			}
			require.GreaterOrEqual(t, len(authors), 2, "authors: %v", authors)
		})
	}
}
