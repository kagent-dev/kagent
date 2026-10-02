package e2e_test

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/mockllm"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// compactionSummaryPrompt is the custom summarizer prompt of the fixture. The
// mock LLM answers a summarization request by this text rather than by the
// runtime's default prompt, whose wording is not a contract.
const compactionSummaryPrompt = "Summarize the conversation for the compaction test."

// compactionSummary is what the mock summarizer model answers. It stands in
// for the compacted turns in every later prompt.
const compactionSummary = "Compacted summary: the codeword is ALPHA and the port is 8443."

// TestE2EInvokeAgentWithContextCompaction verifies that
// spec.declarative.context.compaction reaches the Go runtime and that the
// runtime compacts with it: after the configured number of turns the runtime
// asks the dedicated summarizer model for a summary, and the next turn's model
// request carries that summary in place of the compacted turns. Both models
// are the same mock LLM behind a recording proxy; a default header on each
// ModelConfig tells the two apart.
func TestE2EInvokeAgentWithContextCompaction(t *testing.T) {
	mockllmCfg, err := mockllm.LoadConfigFromFile("mocks/invoke_golang_compaction.json", mocks)
	require.NoError(t, err)
	server := mockllm.NewServer(mockllmCfg)
	localURL, err := server.Start(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := server.Stop(t.Context()); err != nil {
			t.Errorf("failed to stop server: %v", err)
		}
	})
	recorder := startModelRecorder(t, localURL)
	modelURL := buildK8sURL(recorder.URL) + "/v1"

	cli := setupK8sClient(t, false)
	agentModel := setupModelConfigWithHeaders(t, cli, modelURL, map[string]string{"X-Kagent-E2E-Model": "agent"})
	summarizerModel := setupModelConfigWithHeaders(t, cli, modelURL, map[string]string{"X-Kagent-E2E-Model": "summarizer"})

	promptTemplate := compactionSummaryPrompt + "\n\n{conversation_history}"
	agent := setupAgentWithOptions(t, cli, agentModel.Name, nil, AgentOptions{
		Name:          "compaction-test",
		SystemMessage: "Reply briefly.",
		Context: &v1alpha2.ContextConfig{Compaction: &v1alpha2.ContextCompressionConfig{
			CompactionInterval: new(2),
			OverlapSize:        new(0),
			Summarizer: &v1alpha2.ContextSummarizerConfig{
				ModelConfig:    &summarizerModel.Name,
				PromptTemplate: &promptTemplate,
			},
		}},
	})
	requireAgentRuntime(t, cli, agent, v1alpha2.DeclarativeRuntime_Go)
	a2aClient := setupA2AClient(t, agent)

	turns := []struct{ prompt, reply string }{
		{"Turn one: the codeword is ALPHA.", "Noted: the codeword is ALPHA."},
		{"Turn two: the port is 8443.", "Noted: the port is 8443."},
		{"Turn three: repeat the codeword and the port.", "The codeword is ALPHA and the port is 8443."},
	}
	var contextID string
	for _, turn := range turns {
		task := runSyncTest(t, a2aClient, turn.prompt, turn.reply, nil, contextID)
		require.Equal(t, a2atype.TaskStateCompleted, task.Status.State)
		contextID = task.ContextID
	}

	// The sliding window fires once the second invocation is complete, before
	// the runtime answers the turn, and covers both turns so far.
	summaries := recorder.Requests("X-Kagent-E2E-Model", "summarizer")
	require.Len(t, summaries, 1, "the summarizer model is called once after the second turn")
	require.Contains(t, string(summaries[0].Body), compactionSummaryPrompt)
	require.Contains(t, string(summaries[0].Body), turns[0].prompt)
	require.Contains(t, string(summaries[0].Body), turns[1].prompt)

	requests := recorder.Requests("X-Kagent-E2E-Model", "agent")
	require.Len(t, requests, 3, "one agent model call per turn")
	second := string(requests[1].Body)
	require.Contains(t, second, turns[0].prompt, "before the window fires the raw turn is still in the prompt")
	third := string(requests[2].Body)
	require.Contains(t, third, compactionSummary, "the summary stands in for the compacted turns")
	require.Contains(t, third, turns[2].prompt)
	for _, compacted := range []string{turns[0].prompt, turns[0].reply, turns[1].prompt, turns[1].reply} {
		require.NotContains(t, third, compacted, "compacted turn still in the prompt")
	}
}

// setupModelConfigWithHeaders creates a ModelConfig whose requests carry the
// given default headers, so a recording proxy can attribute them.
func setupModelConfigWithHeaders(t *testing.T, cli client.Client, baseURL string, headers map[string]string) *v1alpha2.ModelConfig {
	t.Helper()
	modelCfg := generateModelCfg(baseURL, "gpt-4.1-mini")
	modelCfg.Spec.DefaultHeaders = headers
	require.NoError(t, cli.Create(t.Context(), modelCfg))
	cleanup(t, cli, modelCfg)
	return modelCfg
}

// modelRecorder proxies model requests to a mock LLM and keeps a copy of each
// one, so a test can assert on what the runtime sent rather than only on what
// the mock answered.
type modelRecorder struct {
	// URL is the proxy's listener on the test host.
	URL string

	mu       sync.Mutex
	requests []recordedModelRequest
}

type recordedModelRequest struct {
	Header http.Header
	Body   []byte
}

// startModelRecorder puts a recording proxy in front of the mock LLM at
// upstreamURL and returns its URL on the test host.
func startModelRecorder(t *testing.T, upstreamURL string) *modelRecorder {
	t.Helper()
	upstream, err := url.Parse(upstreamURL)
	require.NoError(t, err)
	recorder := &modelRecorder{}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = r.Body.Close()
		recorder.mu.Lock()
		recorder.requests = append(recorder.requests, recordedModelRequest{Header: r.Header.Clone(), Body: body})
		recorder.mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		proxy.ServeHTTP(w, r)
	}))
	// Listen on every interface: the agent pod reaches the test host through
	// the Docker bridge address buildK8sURL maps to, not through loopback.
	_ = server.Listener.Close()
	server.Listener, err = net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	server.Start()
	t.Cleanup(server.Close)
	recorder.URL = server.URL
	return recorder
}

// Requests returns the recorded requests carrying the header value, in
// arrival order.
func (r *modelRecorder) Requests(header, value string) []recordedModelRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	var matched []recordedModelRequest
	for _, request := range r.requests {
		if request.Header.Get(header) == value {
			matched = append(matched, request)
		}
	}
	return matched
}
