package a2a

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
)

// TestKAgentExecutorFailsATurnTheProviderRateLimits runs a turn against a model
// provider that answers every call 429: the turn ends FAILED, and its status
// message names the rate limit, the retries and the wait.
func TestKAgentExecutorFailsATurnTheProviderRateLimits(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "0")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`)
	}))
	t.Cleanup(provider.Close)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	llm, err := models.NewAnthropicModel(t.Context(), &models.AnthropicConfig{Model: "claude-test", BaseUrl: provider.URL})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := llmagent.New(llmagent.Config{Name: "rate_limited_agent", Model: llm})
	if err != nil {
		t.Fatal(err)
	}
	executor, err := NewKAgentExecutor(KAgentExecutorConfig{
		AppName:        "test-app",
		SessionService: adksession.InMemoryService(),
		Logger:         slog.New(slog.DiscardHandler),
		RunnerConfig:   runner.Config{AppName: "test-app", Agent: agent},
	})
	if err != nil {
		t.Fatal(err)
	}
	reqCtx := &a2asrv.ExecutorContext{
		TaskID: "task-1", ContextID: "context-1",
		Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi")),
	}

	var terminal *a2atype.TaskStatusUpdateEvent
	for event, err := range executor.Execute(t.Context(), reqCtx) {
		if err != nil {
			t.Fatal(err)
		}
		if update, ok := event.(*a2atype.TaskStatusUpdateEvent); ok && update.Status.State.Terminal() {
			terminal = update
		}
	}

	if terminal == nil || terminal.Status.State != a2atype.TaskStateFailed || terminal.Status.Message == nil {
		t.Fatalf("terminal status = %#v, want a failed status with a message", terminal)
	}
	const want = "model provider rate-limited the request (429), retried 3 times, Retry-After 0s"
	if text := terminal.Status.Message.Parts[0].Text(); !strings.Contains(text, want) {
		t.Fatalf("status message = %q, want it to contain %q", text, want)
	}
	if got := calls.Load(); got != 4 {
		t.Fatalf("provider calls = %d, want 4: the first call and three retries", got)
	}
}
