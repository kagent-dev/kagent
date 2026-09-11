package models_test

// End-to-end regression test for STS token injection on the model transport.
//
// Nothing is stubbed except the LLM endpoint: a real runner drives a real
// llmagent over a real OpenAIModel built by NewOpenAIModel (so a real
// http.Client with a non-zero Timeout), a real TokenPropagationPlugin exchanges
// against an httptest STS, and an httptest LLM records every Authorization it
// receives.
//
// It fails when the model transport sends the caller's own token, which is what
// happens with no exchanged-token step in models.PassthroughToken.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	"github.com/kagent-dev/kagent/go/adk/pkg/sts"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	adkplugin "google.golang.org/adk/v2/plugin"
	adkrunner "google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

const (
	llmExchangedToken = "EXCHANGED-STS-TOKEN"
	llmSubjectToken   = "RAW-CALLER-TOKEN"
	llmActorToken     = "AGENT-SA-TOKEN"
	llmSession        = "01a01e53-cfc7-7c25-9783-d0e5203b6451"
	llmAppName        = "llm-sts-injection"
	llmUserID         = "u1"
)

// llmAuthRecorder captures the Authorization header of every request the LLM
// endpoint receives, in order.
type llmAuthRecorder struct {
	mu   sync.Mutex
	seen []string
}

func (a *llmAuthRecorder) record(v string) {
	a.mu.Lock()
	a.seen = append(a.seen, v)
	a.mu.Unlock()
}

func (a *llmAuthRecorder) snapshot() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.seen...)
}

// newRecordingLLMServer serves a minimal chat completion and records auth headers.
func newRecordingLLMServer(t *testing.T) (baseURL string, rec *llmAuthRecorder) {
	t.Helper()
	rec = &llmAuthRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-1",
			"object":  "chat.completion",
			"created": 0,
			"model":   "gpt-4o",
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "done"},
				"finish_reason": "stop",
			}},
		})
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/", rec
}

// newExchangingSTS returns an httptest STS that exchanges any subject token for
// llmExchangedToken, plus a count of exchanges performed.
func newExchangingSTS(t *testing.T) (wellKnownURI string, exchanges *int) {
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
			if got := r.FormValue("subject_token"); got != llmSubjectToken {
				t.Errorf("subject_token = %q, want %q: the STS must be asked to exchange the caller's token", got, llmSubjectToken)
			}
			count++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":      llmExchangedToken,
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

func TestSTSExchangedTokenReachesLLM(t *testing.T) {
	llmURL, rec := newRecordingLLMServer(t)

	wellKnownURI, exchanges := newExchangingSTS(t)
	actorTokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(actorTokenPath, []byte(llmActorToken), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	integration, err := sts.NewSTSIntegration(wellKnownURI, actorTokenPath, nil, nil, 5, true, false)
	if err != nil {
		t.Fatalf("NewSTSIntegration() error = %v", err)
	}
	plugin := sts.NewTokenPropagationPlugin(integration, slog.New(slog.DiscardHandler), nil, nil)

	// The real model transport, with passthrough on as the gateway deployment configures it.
	llm, err := models.NewOpenAIModel(context.Background(), &models.OpenAIConfig{
		TransportConfig: models.TransportConfig{APIKeyPassthrough: true},
		Model:           "gpt-4o",
		BaseUrl:         llmURL,
	})
	if err != nil {
		t.Fatalf("NewOpenAIModel() error = %v", err)
	}

	adkPlugin, err := plugin.ADKPlugin()
	if err != nil {
		t.Fatalf("ADKPlugin() error = %v", err)
	}

	adkAgent, err := llmagent.New(llmagent.Config{
		Name:            "llm_sts_injection_agent",
		Description:     "probe",
		Instruction:     "answer",
		Model:           llm,
		IncludeContents: llmagent.IncludeContentsDefault,
	})
	if err != nil {
		t.Fatalf("llmagent.New() error = %v", err)
	}

	sessionSvc := adksession.InMemoryService()
	r, err := adkrunner.New(adkrunner.Config{
		AppName:        llmAppName,
		Agent:          adkAgent,
		SessionService: sessionSvc,
		PluginConfig:   adkrunner.PluginConfig{Plugins: []*adkplugin.Plugin{adkPlugin}},
	})
	if err != nil {
		t.Fatalf("runner.New() error = %v", err)
	}

	if _, err := sessionSvc.Create(context.Background(), &adksession.CreateRequest{
		AppName: llmAppName, UserID: llmUserID, SessionID: llmSession,
	}); err != nil {
		t.Fatalf("session Create() error = %v", err)
	}

	// The context the A2A executor builds: CallContext, bearer token, session ID,
	// and the exchanged-token provider.
	base, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	ctx, _ := a2asrv.NewCallContext(base, a2asrv.NewServiceParams(
		map[string][]string{"authorization": {"Bearer " + llmSubjectToken}}))
	ctx = context.WithValue(ctx, models.BearerTokenKey, llmSubjectToken)
	ctx = context.WithValue(ctx, models.SessionIDKey, llmSession)
	ctx = context.WithValue(ctx, models.ExchangedTokenProviderKey, models.ExchangedTokenProvider(plugin))

	msg := &genai.Content{Role: "user", Parts: []*genai.Part{{Text: "hello"}}}
	for _, err := range r.Run(ctx, llmUserID, llmSession, msg, adkagent.RunConfig{}) {
		if err != nil {
			t.Fatalf("the ADK run must reach the LLM, error = %v", err)
		}
	}

	seen := rec.snapshot()
	for i, auth := range seen {
		t.Logf("llm request[%d] Authorization = %q", i, auth)
	}

	if len(seen) == 0 {
		t.Fatal("the invocation made no LLM request")
	}
	if *exchanges != 1 {
		t.Errorf("STS exchanges = %d, want 1 for one session", *exchanges)
	}
	for i, auth := range seen {
		if auth != "Bearer "+llmExchangedToken {
			t.Errorf("llm request %d Authorization = %q, want %q: it must carry the EXCHANGED token, not the caller's raw token",
				i, auth, "Bearer "+llmExchangedToken)
		}
	}
}
