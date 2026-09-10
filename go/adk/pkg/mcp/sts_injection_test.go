package mcp

// End-to-end regression test for STS token injection on the MCP transport.
//
// Nothing is stubbed except the LLM: a real runner drives a real llmagent and
// mcptoolset through CreateToolsets/createTransport (so a real http.Client with a
// non-zero Timeout), a real TokenPropagationPlugin exchanges against an httptest
// STS, and a real MCP server records every Authorization it receives.
//
// It fails when the session is recovered by type assertion alone, which stops
// matching once http.Client has wrapped the request context in a deadline.

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"log/slog"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	"github.com/kagent-dev/kagent/go/adk/pkg/sts"
	"github.com/kagent-dev/kagent/go/api/adk"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	adkmodel "google.golang.org/adk/v2/model"
	adkplugin "google.golang.org/adk/v2/plugin"
	adkrunner "google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

const (
	stsInjectionTool     = "echoProbe"
	stsExchangedToken    = "EXCHANGED-STS-TOKEN"
	stsSubjectToken      = "RAW-CALLER-TOKEN"
	stsActorToken        = "AGENT-SA-TOKEN"
	stsInjectionSession  = "01a01e53-cfc7-7c25-9783-d0e5203b6451"
	stsInjectionAppName  = "sts-injection"
	stsInjectionUserID   = "u1"
	stsInjectionMCPLimit = 5.0 // seconds; any non-zero value adds the deadline wrapper
)

// authRecorder captures the Authorization header of every request the MCP server
// receives, in order.
type authRecorder struct {
	mu   sync.Mutex
	seen []string
}

func (a *authRecorder) record(v string) {
	a.mu.Lock()
	a.seen = append(a.seen, v)
	a.mu.Unlock()
}

func (a *authRecorder) snapshot() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.seen...)
}

// stsInjectionLLM asks for the MCP tool once, then answers with text.
type stsInjectionLLM struct {
	mu    sync.Mutex
	calls int
}

func (f *stsInjectionLLM) Name() string { return "fake-llm" }

func (f *stsInjectionLLM) GenerateContent(ctx context.Context, req *adkmodel.LLMRequest, stream bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.mu.Unlock()

	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		part := &genai.Part{Text: "done"}
		if n == 1 {
			part = &genai.Part{FunctionCall: &genai.FunctionCall{
				ID:   "fc-1",
				Name: stsInjectionTool,
				Args: map[string]any{},
			}}
		}
		yield(&adkmodel.LLMResponse{
			Content:      &genai.Content{Role: "model", Parts: []*genai.Part{part}},
			TurnComplete: true,
		}, nil)
	}
}

// newFakeSTS returns an httptest STS that exchanges any subject token for
// stsExchangedToken, plus a count of exchanges performed.
func newFakeSTS(t *testing.T) (wellKnownURI string, exchanges *int) {
	t.Helper()
	count := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":         srv.URL,
				"token_endpoint": srv.URL + "/token",
			})
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm() error = %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if got := r.FormValue("subject_token"); got != stsSubjectToken {
				t.Errorf("subject_token = %q, want %q: the STS must be asked to exchange the caller's token", got, stsSubjectToken)
			}
			count++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":      stsExchangedToken,
				"issued_token_type": string(sts.TokenTypeJWT),
				"expires_in":        3600,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/.well-known/oauth-authorization-server", &count
}

func TestSTSExchangedTokenReachesMCPTool(t *testing.T) {
	rec := &authRecorder{}

	// --- a real MCP server over streamable HTTP, recording what it receives
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "sts-injection", Version: "0"}, nil)
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: stsInjectionTool, Description: "probe"},
		func(ctx context.Context, req *mcpsdk.CallToolRequest, in struct{}) (*mcpsdk.CallToolResult, struct{}, error) {
			return &mcpsdk.CallToolResult{}, struct{}{}, nil
		})
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return srv }, nil)
	mcpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r.Header.Get("Authorization"))
		handler.ServeHTTP(w, r)
	}))
	defer mcpSrv.Close()

	// --- a real STS integration, reading its actor token from disk as in a pod
	wellKnownURI, exchanges := newFakeSTS(t)
	actorTokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(actorTokenPath, []byte(stsActorToken), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	integration, err := sts.NewSTSIntegration(wellKnownURI, actorTokenPath, nil, nil, 5, true, false)
	if err != nil {
		t.Fatalf("NewSTSIntegration() error = %v", err)
	}

	plugin := sts.NewTokenPropagationPlugin(integration, slog.New(slog.DiscardHandler), nil, nil)

	// --- the real toolset construction, with the real header provider. The short
	// timeout only keeps the SSE stream from holding the test open; any non-zero
	// value produces the deadline wrapper under test.
	mcpTimeout := stsInjectionMCPLimit
	toolsets := CreateToolsets(
		context.Background(),
		[]adk.HttpMcpServerConfig{{Params: adk.StreamableHTTPConnectionParams{
			Url:     mcpSrv.URL,
			Timeout: &mcpTimeout,
		}}},
		nil,
		nil,
		false, // propagateToken off: isolate the HeaderProvider path
		plugin.HeaderProvider,
	)
	if len(toolsets) != 1 {
		t.Fatalf("CreateToolsets() = %d toolsets, want 1 against the probe MCP server", len(toolsets))
	}

	// Everything so far is startup discovery: no user in context, so no header.
	discovery := rec.snapshot()
	if len(discovery) == 0 {
		t.Fatal("startup discovery should have contacted the MCP server")
	}
	for i, auth := range discovery {
		if auth != "" {
			t.Errorf("startup discovery request %d Authorization = %q, want empty", i, auth)
		}
	}
	if *exchanges != 0 {
		t.Errorf("STS exchanges during discovery = %d, want 0", *exchanges)
	}

	// --- a real agent and runner, so BeforeRunCallback performs the real exchange.
	adkPlugin, err := plugin.ADKPlugin()
	if err != nil {
		t.Fatalf("ADKPlugin() error = %v", err)
	}

	adkAgent, err := llmagent.New(llmagent.Config{
		Name:            "sts_injection_agent",
		Description:     "probe",
		Instruction:     "call the tool",
		Model:           &stsInjectionLLM{},
		IncludeContents: llmagent.IncludeContentsDefault,
		Toolsets:        toolsets,
	})
	if err != nil {
		t.Fatalf("llmagent.New() error = %v", err)
	}

	sessionSvc := adksession.InMemoryService()
	r, err := adkrunner.New(adkrunner.Config{
		AppName:        stsInjectionAppName,
		Agent:          adkAgent,
		SessionService: sessionSvc,
		PluginConfig:   adkrunner.PluginConfig{Plugins: []*adkplugin.Plugin{adkPlugin}},
	})
	if err != nil {
		t.Fatalf("runner.New() error = %v", err)
	}

	if _, err := sessionSvc.Create(context.Background(), &adksession.CreateRequest{
		AppName: stsInjectionAppName, UserID: stsInjectionUserID, SessionID: stsInjectionSession,
	}); err != nil {
		t.Fatalf("session Create() error = %v", err)
	}

	// --- the context the A2A executor builds: CallContext, bearer token, session ID.
	base, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	ctx, _ := a2asrv.NewCallContext(base, a2asrv.NewServiceParams(
		map[string][]string{"authorization": {"Bearer " + stsSubjectToken}}))
	ctx = context.WithValue(ctx, models.BearerTokenKey, stsSubjectToken)
	ctx = context.WithValue(ctx, models.SessionIDKey, stsInjectionSession)

	msg := &genai.Content{Role: "user", Parts: []*genai.Part{{Text: "please call the tool"}}}
	for _, err := range r.Run(ctx, stsInjectionUserID, stsInjectionSession, msg, adkagent.RunConfig{}) {
		if err != nil {
			t.Fatalf("the ADK run must reach the tool call, error = %v", err)
		}
	}

	all := rec.snapshot()
	invocation := all[len(discovery):]
	for i, auth := range discovery {
		t.Logf("discovery[%d]  Authorization = %q", i, auth)
	}
	for i, auth := range invocation {
		t.Logf("invocation[%d] Authorization = %q", i, auth)
	}

	if len(invocation) == 0 {
		t.Fatal("the invocation made no MCP request")
	}
	if *exchanges != 1 {
		t.Errorf("STS exchanges = %d, want 1 for one session", *exchanges)
	}

	// With session recovery by type assertion alone, every one of these is "".
	for i, auth := range invocation {
		if auth != "Bearer "+stsExchangedToken {
			t.Errorf("invocation request %d Authorization = %q, want %q: it must carry the EXCHANGED token, not the caller's raw token",
				i, auth, "Bearer "+stsExchangedToken)
		}
	}
}
