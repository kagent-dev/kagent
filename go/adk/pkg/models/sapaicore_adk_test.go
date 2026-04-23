package models

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// ---- helpers ----

func newTestSAPModel(t *testing.T, baseURL, authURL string) *SAPAICoreModel {
	t.Helper()
	m, err := NewSAPAICoreModelWithLogger(SAPAICoreConfig{
		Model:         "test-model",
		BaseUrl:       baseURL,
		ResourceGroup: "default",
		AuthUrl:       authURL,
	}, logr.Discard())
	if err != nil {
		t.Fatalf("NewSAPAICoreModelWithLogger: %v", err)
	}
	return m
}

func oauthServer(t *testing.T, token string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": token,
			"expires_in":   3600,
		})
	}))
}

func deploymentServerWith(urls ...string) *httptest.Server {
	resources := make([]map[string]any, 0, len(urls))
	for i, u := range urls {
		resources = append(resources, map[string]any{
			"id":            fmt.Sprintf("dep-%d", i),
			"scenarioId":    "orchestration",
			"status":        "RUNNING",
			"deploymentUrl": u,
			"createdAt":     fmt.Sprintf("2024-01-%02d", i+1),
		})
	}
	body, _ := json.Marshal(map[string]any{"resources": resources})
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
}

// ---- genaiContentsToOrchTemplate ----

func TestGenaiContentsToOrchTemplate_Empty(t *testing.T) {
	msgs, sys := genaiContentsToOrchTemplate(nil, nil)
	if len(msgs) != 0 {
		t.Errorf("len(msgs) = %d, want 0", len(msgs))
	}
	if sys != "" {
		t.Errorf("sys = %q, want empty", sys)
	}
}

func TestGenaiContentsToOrchTemplate_SystemInstruction(t *testing.T) {
	config := &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{
				{Text: "You are helpful."},
				{Text: "Be concise."},
			},
		},
	}
	_, sys := genaiContentsToOrchTemplate(nil, config)
	want := "You are helpful.\nBe concise."
	if sys != want {
		t.Errorf("sys = %q, want %q", sys, want)
	}
}

func TestGenaiContentsToOrchTemplate_TextMessages(t *testing.T) {
	contents := []*genai.Content{
		{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
		{Role: "model", Parts: []*genai.Part{{Text: "Hi there"}}},
	}
	msgs, sys := genaiContentsToOrchTemplate(contents, nil)
	if sys != "" {
		t.Errorf("sys = %q, want empty", sys)
	}
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2", len(msgs))
	}
	if msgs[0]["role"] != "user" || msgs[0]["content"] != "Hello" {
		t.Errorf("msgs[0] = %v, want {role:user, content:Hello}", msgs[0])
	}
	if msgs[1]["role"] != "assistant" || msgs[1]["content"] != "Hi there" {
		t.Errorf("msgs[1] = %v, want {role:assistant, content:Hi there}", msgs[1])
	}
}

func TestGenaiContentsToOrchTemplate_SkipsSystemRole(t *testing.T) {
	contents := []*genai.Content{
		{Role: "system", Parts: []*genai.Part{{Text: "ignored"}}},
		{Role: "user", Parts: []*genai.Part{{Text: "hello"}}},
	}
	msgs, _ := genaiContentsToOrchTemplate(contents, nil)
	if len(msgs) != 1 {
		t.Errorf("len(msgs) = %d, want 1 (system role skipped)", len(msgs))
	}
}

func TestGenaiContentsToOrchTemplate_SkipsNilContent(t *testing.T) {
	contents := []*genai.Content{
		nil,
		{Role: "user", Parts: []*genai.Part{{Text: "hello"}}},
	}
	msgs, _ := genaiContentsToOrchTemplate(contents, nil)
	if len(msgs) != 1 {
		t.Errorf("len(msgs) = %d, want 1 (nil content skipped)", len(msgs))
	}
}

func TestGenaiContentsToOrchTemplate_ToolCall(t *testing.T) {
	fc := genai.NewPartFromFunctionCall("get_weather", map[string]any{"city": "Berlin"})
	fc.FunctionCall.ID = "call_1"

	contents := []*genai.Content{
		{Role: "model", Parts: []*genai.Part{fc}},
	}
	msgs, _ := genaiContentsToOrchTemplate(contents, nil)
	if len(msgs) == 0 {
		t.Fatal("expected at least 1 message")
	}
	msg := msgs[0]
	if msg["role"] != "assistant" {
		t.Errorf("role = %v, want assistant", msg["role"])
	}
	toolCalls, ok := msg["tool_calls"].([]map[string]any)
	if !ok || len(toolCalls) == 0 {
		t.Fatalf("tool_calls = %v, want non-empty slice", msg["tool_calls"])
	}
	if toolCalls[0]["id"] != "call_1" {
		t.Errorf("tool_calls[0].id = %v, want call_1", toolCalls[0]["id"])
	}
}

func TestGenaiContentsToOrchTemplate_FunctionResponse(t *testing.T) {
	fc := genai.NewPartFromFunctionCall("get_weather", map[string]any{"city": "Berlin"})
	fc.FunctionCall.ID = "call_1"
	fr := genai.NewPartFromFunctionResponse("get_weather", map[string]any{"temp": "20C"})
	fr.FunctionResponse.ID = "call_1"

	contents := []*genai.Content{
		{Role: "model", Parts: []*genai.Part{fc}},
		{Role: "user", Parts: []*genai.Part{fr}},
	}
	msgs, _ := genaiContentsToOrchTemplate(contents, nil)

	if len(msgs) < 2 {
		t.Fatalf("len(msgs) = %d, want >= 2", len(msgs))
	}
	toolMsg := msgs[len(msgs)-1]
	if toolMsg["role"] != "tool" {
		t.Errorf("last msg role = %v, want tool", toolMsg["role"])
	}
	if toolMsg["tool_call_id"] != "call_1" {
		t.Errorf("tool_call_id = %v, want call_1", toolMsg["tool_call_id"])
	}
}

// ---- buildOrchestrationBody ----

func TestBuildOrchestrationBody_Basic(t *testing.T) {
	m := &SAPAICoreModel{Config: SAPAICoreConfig{Model: "my-model"}}
	req := &model.LLMRequest{Model: "my-model"}
	body := m.buildOrchestrationBody(req, false)

	cfg, ok := body["config"].(map[string]any)
	if !ok {
		t.Fatalf("body[config] missing or wrong type")
	}
	modules, ok := cfg["modules"].(map[string]any)
	if !ok {
		t.Fatalf("config[modules] missing")
	}
	if _, ok := modules["prompt_templating"]; !ok {
		t.Error("modules[prompt_templating] missing")
	}
	stream, ok := cfg["stream"].(map[string]any)
	if !ok {
		t.Fatalf("config[stream] missing")
	}
	if stream["enabled"] != false {
		t.Errorf("stream.enabled = %v, want false", stream["enabled"])
	}
}

func TestBuildOrchestrationBody_StreamEnabled(t *testing.T) {
	m := &SAPAICoreModel{Config: SAPAICoreConfig{Model: "my-model"}}
	body := m.buildOrchestrationBody(&model.LLMRequest{}, true)
	cfg := body["config"].(map[string]any)
	stream := cfg["stream"].(map[string]any)
	if stream["enabled"] != true {
		t.Errorf("stream.enabled = %v, want true", stream["enabled"])
	}
}

func TestBuildOrchestrationBody_Params(t *testing.T) {
	m := &SAPAICoreModel{Config: SAPAICoreConfig{Model: "my-model"}}
	temp := float32(0.7)
	topP := float32(0.9)
	req := &model.LLMRequest{
		Config: &genai.GenerateContentConfig{
			Temperature:     &temp,
			MaxOutputTokens: 512,
			TopP:            &topP,
		},
	}
	body := m.buildOrchestrationBody(req, false)

	cfg := body["config"].(map[string]any)
	modules := cfg["modules"].(map[string]any)
	pt := modules["prompt_templating"].(map[string]any)
	modelBlock := pt["model"].(map[string]any)
	params := modelBlock["params"].(map[string]any)

	if params["temperature"] != float32(0.7) {
		t.Errorf("temperature = %v, want 0.7", params["temperature"])
	}
	if params["max_tokens"] != int32(512) {
		t.Errorf("max_tokens = %v, want 512", params["max_tokens"])
	}
	if params["top_p"] != float32(0.9) {
		t.Errorf("top_p = %v, want 0.9", params["top_p"])
	}
}

func TestBuildOrchestrationBody_WithTools(t *testing.T) {
	m := &SAPAICoreModel{Config: SAPAICoreConfig{Model: "my-model"}}
	req := &model.LLMRequest{
		Config: &genai.GenerateContentConfig{
			Tools: []*genai.Tool{{
				FunctionDeclarations: []*genai.FunctionDeclaration{{
					Name:        "list_pods",
					Description: "List pods",
				}},
			}},
		},
	}
	body := m.buildOrchestrationBody(req, false)
	cfg := body["config"].(map[string]any)
	modules := cfg["modules"].(map[string]any)
	pt := modules["prompt_templating"].(map[string]any)
	prompt := pt["prompt"].(map[string]any)
	tools, ok := prompt["tools"].([]map[string]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("prompt[tools] = %v, want non-empty", prompt["tools"])
	}
	fn := tools[0]["function"].(map[string]any)
	if fn["name"] != "list_pods" {
		t.Errorf("tool name = %v, want list_pods", fn["name"])
	}
}

// ---- parseOrchChunk ----

func TestParseOrchChunk(t *testing.T) {
	tests := []struct {
		name    string
		event   map[string]any
		wantNil bool
		wantKey string
	}{
		{
			name:    "orchestration_result",
			event:   map[string]any{"orchestration_result": map[string]any{"choices": []any{}}},
			wantNil: false,
			wantKey: "choices",
		},
		{
			name:    "final_result",
			event:   map[string]any{"final_result": map[string]any{"choices": []any{}}},
			wantNil: false,
			wantKey: "choices",
		},
		{
			name:    "direct choices",
			event:   map[string]any{"choices": []any{}, "other": "data"},
			wantNil: false,
			wantKey: "choices",
		},
		{
			name:    "unrecognized",
			event:   map[string]any{"foo": "bar"},
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseOrchChunk(tt.event)
			if tt.wantNil {
				if got != nil {
					t.Errorf("parseOrchChunk() = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("parseOrchChunk() = nil, want non-nil")
			}
			if _, ok := got[tt.wantKey]; !ok {
				t.Errorf("result missing key %q", tt.wantKey)
			}
		})
	}
}

// ---- isRetryableError ----

func TestIsRetryableError(t *testing.T) {
	retryable := []int{401, 403, 404, 502, 503, 504}
	for _, code := range retryable {
		t.Run(fmt.Sprintf("HTTP_%d_retryable", code), func(t *testing.T) {
			if !isRetryableError(&orchHTTPError{StatusCode: code}) {
				t.Errorf("isRetryableError(HTTP %d) = false, want true", code)
			}
		})
	}
	nonRetryable := []int{400, 422, 500}
	for _, code := range nonRetryable {
		t.Run(fmt.Sprintf("HTTP_%d_not_retryable", code), func(t *testing.T) {
			if isRetryableError(&orchHTTPError{StatusCode: code}) {
				t.Errorf("isRetryableError(HTTP %d) = true, want false", code)
			}
		})
	}
	t.Run("non-HTTP error not retryable", func(t *testing.T) {
		if isRetryableError(fmt.Errorf("network error")) {
			t.Error("isRetryableError(non-HTTP) = true, want false")
		}
	})
}

// ---- ensureToken (OAuth token caching) ----

func TestEnsureToken_CachesToken(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok-cached",
			"expires_in":   3600,
		})
	}))
	defer srv.Close()

	t.Setenv("SAP_AI_CORE_CLIENT_ID", "id")
	t.Setenv("SAP_AI_CORE_CLIENT_SECRET", "secret")

	m := newTestSAPModel(t, "http://base", srv.URL)
	ctx := context.Background()

	tok1, err := m.ensureToken(ctx)
	if err != nil {
		t.Fatalf("ensureToken first call: %v", err)
	}
	tok2, err := m.ensureToken(ctx)
	if err != nil {
		t.Fatalf("ensureToken second call: %v", err)
	}
	if tok1 != "tok-cached" || tok2 != "tok-cached" {
		t.Errorf("tokens = %q, %q, want tok-cached for both", tok1, tok2)
	}
	if callCount != 1 {
		t.Errorf("auth server called %d times, want 1 (cached)", callCount)
	}
}

func TestEnsureToken_RefreshesExpiredToken(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": fmt.Sprintf("tok-%d", callCount),
			"expires_in":   3600,
		})
	}))
	defer srv.Close()

	t.Setenv("SAP_AI_CORE_CLIENT_ID", "id")
	t.Setenv("SAP_AI_CORE_CLIENT_SECRET", "secret")

	m := newTestSAPModel(t, "http://base", srv.URL)
	ctx := context.Background()

	if _, err := m.ensureToken(ctx); err != nil {
		t.Fatalf("first ensureToken: %v", err)
	}

	// Force expiry
	m.mu.Lock()
	m.tokenExpiresAt = time.Now().Add(-1 * time.Second)
	m.mu.Unlock()

	if _, err := m.ensureToken(ctx); err != nil {
		t.Fatalf("second ensureToken: %v", err)
	}
	if callCount != 2 {
		t.Errorf("auth server called %d times, want 2 (expired token refreshed)", callCount)
	}
}

func TestEnsureToken_MissingEnvVarsReturnsError(t *testing.T) {
	os.Unsetenv("SAP_AI_CORE_CLIENT_ID")
	os.Unsetenv("SAP_AI_CORE_CLIENT_SECRET")

	m := newTestSAPModel(t, "http://base", "http://auth")
	_, err := m.ensureToken(context.Background())
	if err == nil {
		t.Error("ensureToken() = nil, want error when env vars missing")
	}
}

func TestInvalidateToken_ClearsCache(t *testing.T) {
	m := newTestSAPModel(t, "http://base", "http://auth")
	m.token = "old-token"
	m.tokenExpiresAt = time.Now().Add(time.Hour)
	m.invalidateToken()
	if m.token != "" {
		t.Errorf("token = %q after invalidate, want empty", m.token)
	}
	if !m.tokenExpiresAt.IsZero() {
		t.Errorf("tokenExpiresAt = %v after invalidate, want zero", m.tokenExpiresAt)
	}
}

// ---- resolveDeploymentURL (URL discovery & caching) ----

func TestResolveDeploymentURL_CachesURL(t *testing.T) {
	authSrv := oauthServer(t, "tok")
	defer authSrv.Close()

	callCount := 0
	depSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := json.Marshal(map[string]any{"resources": []map[string]any{{
			"scenarioId": "orchestration", "status": "RUNNING",
			"deploymentUrl": "https://dep.example.com", "createdAt": "2024-01-01",
		}}})
		w.Write(body)
	}))
	defer depSrv.Close()

	t.Setenv("SAP_AI_CORE_CLIENT_ID", "id")
	t.Setenv("SAP_AI_CORE_CLIENT_SECRET", "secret")

	m := newTestSAPModel(t, depSrv.URL, authSrv.URL)
	ctx := context.Background()

	url1, err := m.resolveDeploymentURL(ctx)
	if err != nil {
		t.Fatalf("first resolveDeploymentURL: %v", err)
	}
	url2, err := m.resolveDeploymentURL(ctx)
	if err != nil {
		t.Fatalf("second resolveDeploymentURL: %v", err)
	}
	if url1 != "https://dep.example.com" || url2 != "https://dep.example.com" {
		t.Errorf("urls = %q, %q, want https://dep.example.com for both", url1, url2)
	}
	if callCount != 1 {
		t.Errorf("deployment API called %d times, want 1 (cached)", callCount)
	}
}

func TestResolveDeploymentURL_PicksLatestCreated(t *testing.T) {
	authSrv := oauthServer(t, "tok")
	defer authSrv.Close()

	depSrv := deploymentServerWith("https://older.example.com", "https://newer.example.com")
	defer depSrv.Close()

	t.Setenv("SAP_AI_CORE_CLIENT_ID", "id")
	t.Setenv("SAP_AI_CORE_CLIENT_SECRET", "secret")

	m := newTestSAPModel(t, depSrv.URL, authSrv.URL)
	url, err := m.resolveDeploymentURL(context.Background())
	if err != nil {
		t.Fatalf("resolveDeploymentURL: %v", err)
	}
	// "2024-01-02" > "2024-01-01" — newer should win
	if url != "https://newer.example.com" {
		t.Errorf("url = %q, want https://newer.example.com", url)
	}
}

func TestResolveDeploymentURL_NoRunningDeploymentError(t *testing.T) {
	authSrv := oauthServer(t, "tok")
	defer authSrv.Close()

	depSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := json.Marshal(map[string]any{"resources": []map[string]any{{
			"scenarioId": "other-scenario", "status": "RUNNING",
			"deploymentUrl": "https://x.example.com", "createdAt": "2024-01-01",
		}}})
		w.Write(body)
	}))
	defer depSrv.Close()

	t.Setenv("SAP_AI_CORE_CLIENT_ID", "id")
	t.Setenv("SAP_AI_CORE_CLIENT_SECRET", "secret")

	m := newTestSAPModel(t, depSrv.URL, authSrv.URL)
	_, err := m.resolveDeploymentURL(context.Background())
	if err == nil {
		t.Error("resolveDeploymentURL() = nil, want error for no running orchestration deployments")
	}
}

func TestResolveDeploymentURL_ExpiresAfterOneHour(t *testing.T) {
	authSrv := oauthServer(t, "tok")
	defer authSrv.Close()

	callCount := 0
	depSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := json.Marshal(map[string]any{"resources": []map[string]any{{
			"scenarioId": "orchestration", "status": "RUNNING",
			"deploymentUrl": "https://dep.example.com", "createdAt": "2024-01-01",
		}}})
		w.Write(body)
	}))
	defer depSrv.Close()

	t.Setenv("SAP_AI_CORE_CLIENT_ID", "id")
	t.Setenv("SAP_AI_CORE_CLIENT_SECRET", "secret")

	m := newTestSAPModel(t, depSrv.URL, authSrv.URL)
	ctx := context.Background()

	// First call — populates cache.
	if _, err := m.resolveDeploymentURL(ctx); err != nil {
		t.Fatalf("first resolveDeploymentURL: %v", err)
	}

	// Expire the cache by backdating the timestamp.
	m.mu.Lock()
	m.deploymentURLAt = time.Now().Add(-2 * time.Hour)
	m.mu.Unlock()

	// Second call — cache expired, must re-fetch.
	url, err := m.resolveDeploymentURL(ctx)
	if err != nil {
		t.Fatalf("second resolveDeploymentURL: %v", err)
	}
	if url != "https://dep.example.com" {
		t.Errorf("url = %q, want https://dep.example.com", url)
	}
	if callCount != 2 {
		t.Errorf("deployment API called %d times, want 2 (cache expired)", callCount)
	}
}

func TestInvalidateDeploymentURL_ClearsCache(t *testing.T) {
	m := newTestSAPModel(t, "http://base", "http://auth")
	m.deploymentURL = "https://old.example.com"
	m.deploymentURLAt = time.Now()
	m.invalidateDeploymentURL()
	if m.deploymentURL != "" {
		t.Errorf("deploymentURL = %q after invalidate, want empty", m.deploymentURL)
	}
}

// ---- context cancellation ----

func TestEnsureToken_RespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(500 * time.Millisecond):
			json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 3600})
		}
	}))
	defer srv.Close()

	t.Setenv("SAP_AI_CORE_CLIENT_ID", "id")
	t.Setenv("SAP_AI_CORE_CLIENT_SECRET", "secret")

	m := newTestSAPModel(t, "http://base", srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := m.ensureToken(ctx)
	if err == nil {
		t.Error("ensureToken() = nil, want error when context cancelled")
	}
}

// ---- concurrent token access ----

func TestEnsureToken_ConcurrentAccess(t *testing.T) {
	var mu sync.Mutex
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 3600})
	}))
	defer srv.Close()

	t.Setenv("SAP_AI_CORE_CLIENT_ID", "id")
	t.Setenv("SAP_AI_CORE_CLIENT_SECRET", "secret")

	m := newTestSAPModel(t, "http://base", srv.URL)
	ctx := context.Background()

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			if _, err := m.ensureToken(ctx); err != nil {
				t.Errorf("ensureToken concurrent: %v", err)
			}
		})
	}
	wg.Wait()

	// ensureToken holds the mutex while doing HTTP, so only 1 request is made
	// even under concurrent access.
	mu.Lock()
	defer mu.Unlock()
	if callCount != 1 {
		t.Errorf("auth server called %d times, want 1 (mutex serializes concurrent requests)", callCount)
	}
}

// ---- handleNonStream ----

func TestHandleNonStream_TextResponse(t *testing.T) {
	m := newTestSAPModel(t, "http://base", "http://auth")
	body := map[string]any{
		"final_result": map[string]any{
			"choices": []any{
				map[string]any{
					"finish_reason": "stop",
					"message": map[string]any{
						"role":    "assistant",
						"content": "Hello from SAP AI Core",
					},
				},
			},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	var got *model.LLMResponse
	m.handleNonStream(jsonReader(bodyBytes), func(r *model.LLMResponse, err error) bool {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got = r
		return true
	})
	if got == nil {
		t.Fatal("got nil response")
	}
	if got.Content == nil || len(got.Content.Parts) == 0 {
		t.Fatal("got empty content parts")
	}
	if got.Content.Parts[0].Text != "Hello from SAP AI Core" {
		t.Errorf("text = %q, want Hello from SAP AI Core", got.Content.Parts[0].Text)
	}
}

func TestHandleNonStream_ToolCallResponse(t *testing.T) {
	m := newTestSAPModel(t, "http://base", "http://auth")
	body := map[string]any{
		"choices": []any{
			map[string]any{
				"finish_reason": "tool_calls",
				"message": map[string]any{
					"role":    "assistant",
					"content": "",
					"tool_calls": []any{
						map[string]any{
							"id":   "call_1",
							"type": "function",
							"function": map[string]any{
								"name":      "get_weather",
								"arguments": `{"city":"Berlin"}`,
							},
						},
					},
				},
			},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	var got *model.LLMResponse
	m.handleNonStream(jsonReader(bodyBytes), func(r *model.LLMResponse, err error) bool {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got = r
		return true
	})
	if got == nil || got.Content == nil {
		t.Fatal("got nil response or content")
	}
	var fc *genai.FunctionCall
	for _, p := range got.Content.Parts {
		if p.FunctionCall != nil {
			fc = p.FunctionCall
			break
		}
	}
	if fc == nil {
		t.Fatal("no function call part in response")
	}
	if fc.Name != "get_weather" {
		t.Errorf("function name = %q, want get_weather", fc.Name)
	}
	if fc.ID != "call_1" {
		t.Errorf("function ID = %q, want call_1", fc.ID)
	}
}

func TestHandleNonStream_NoChoicesError(t *testing.T) {
	m := newTestSAPModel(t, "http://base", "http://auth")
	body := map[string]any{"choices": []any{}}
	bodyBytes, _ := json.Marshal(body)

	var got *model.LLMResponse
	m.handleNonStream(jsonReader(bodyBytes), func(r *model.LLMResponse, err error) bool {
		got = r
		return true
	})
	if got == nil {
		t.Fatal("expected error response, got nil")
	}
	if got.ErrorCode == "" {
		t.Error("expected non-empty ErrorCode for empty choices")
	}
}

// ---- genaiToolsToOrchTools ----

func TestGenaiToolsToOrchTools_NilAndEmpty(t *testing.T) {
	if got := genaiToolsToOrchTools(nil); len(got) != 0 {
		t.Errorf("genaiToolsToOrchTools(nil) = %v, want empty", got)
	}
	if got := genaiToolsToOrchTools([]*genai.Tool{}); len(got) != 0 {
		t.Errorf("genaiToolsToOrchTools([]) = %v, want empty", got)
	}
}

func TestGenaiToolsToOrchTools_SkipsNilTool(t *testing.T) {
	tools := []*genai.Tool{
		nil,
		{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "foo"}}},
	}
	got := genaiToolsToOrchTools(tools)
	if len(got) != 1 {
		t.Errorf("len(got) = %d, want 1 (nil tool skipped)", len(got))
	}
}

func TestGenaiToolsToOrchTools_WithJsonSchema(t *testing.T) {
	tools := []*genai.Tool{{
		FunctionDeclarations: []*genai.FunctionDeclaration{{
			Name:        "list_namespaces",
			Description: "List K8s namespaces",
			ParametersJsonSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"label": map[string]any{"type": "string"}},
			},
		}},
	}}
	got := genaiToolsToOrchTools(tools)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	fn := got[0]["function"].(map[string]any)
	if fn["name"] != "list_namespaces" {
		t.Errorf("name = %v, want list_namespaces", fn["name"])
	}
	if fn["description"] != "List K8s namespaces" {
		t.Errorf("description = %v", fn["description"])
	}
}

// ---- handleStream ----

// sseBody builds a minimal SSE byte stream from a slice of JSON-serialisable
// payloads. Each entry is written as "data: <json>\n\n"; the stream ends with
// "data: [DONE]\n\n".
func sseBody(t *testing.T, payloads ...any) *strings.Reader {
	t.Helper()
	var b strings.Builder
	for _, p := range payloads {
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("sseBody marshal: %v", err)
		}
		b.WriteString("data: ")
		b.Write(raw)
		b.WriteString("\n\n")
	}
	b.WriteString("data: [DONE]\n\n")
	return strings.NewReader(b.String())
}

// orchChunk wraps a choices slice in the orchestration_result envelope that
// the SAP Orchestration Service uses for streaming responses.
func orchChunk(choices []any) map[string]any {
	return map[string]any{
		"orchestration_result": map[string]any{
			"choices": choices,
		},
	}
}

func textDelta(text string) map[string]any {
	return map[string]any{
		"delta": map[string]any{"content": text},
	}
}

func finishDelta(reason string) map[string]any {
	return map[string]any{
		"delta":         map[string]any{},
		"finish_reason": reason,
	}
}

func TestHandleStream_TextChunks(t *testing.T) {
	m := newTestSAPModel(t, "http://base", "http://auth")

	body := sseBody(t,
		orchChunk([]any{textDelta("Hello")}),
		orchChunk([]any{textDelta(", world")}),
		orchChunk([]any{finishDelta("stop")}),
	)

	var responses []*model.LLMResponse
	m.handleStream(context.Background(), body, func(r *model.LLMResponse, err error) bool {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		responses = append(responses, r)
		return true
	})

	// Expect two partial responses + one final response.
	if len(responses) < 3 {
		t.Fatalf("got %d responses, want >= 3 (2 partials + 1 final)", len(responses))
	}

	// First two are partial text chunks.
	if !responses[0].Partial || responses[0].Content.Parts[0].Text != "Hello" {
		t.Errorf("responses[0]: partial=%v text=%q, want partial=true text=Hello",
			responses[0].Partial, responses[0].Content.Parts[0].Text)
	}
	if !responses[1].Partial || responses[1].Content.Parts[0].Text != ", world" {
		t.Errorf("responses[1]: partial=%v text=%q, want partial=true text=', world'",
			responses[1].Partial, responses[1].Content.Parts[0].Text)
	}

	// Last response is the final aggregated one.
	final := responses[len(responses)-1]
	if final.Partial || !final.TurnComplete {
		t.Errorf("final: partial=%v turnComplete=%v, want partial=false turnComplete=true",
			final.Partial, final.TurnComplete)
	}
	if final.Content == nil || len(final.Content.Parts) == 0 {
		t.Fatal("final response has no content parts")
	}
	if final.Content.Parts[0].Text != "Hello, world" {
		t.Errorf("final aggregated text = %q, want 'Hello, world'", final.Content.Parts[0].Text)
	}
}

func TestHandleStream_ToolCall(t *testing.T) {
	m := newTestSAPModel(t, "http://base", "http://auth")

	// Two chunks that together build up a single tool call (as real SSE streams do).
	chunk1 := orchChunk([]any{map[string]any{
		"delta": map[string]any{
			"tool_calls": []any{map[string]any{
				"index": float64(0),
				"id":    "call_42",
				"function": map[string]any{
					"name":      "get_weather",
					"arguments": `{"city":`,
				},
			}},
		},
	}})
	chunk2 := orchChunk([]any{map[string]any{
		"delta": map[string]any{
			"tool_calls": []any{map[string]any{
				"index": float64(0),
				"function": map[string]any{
					"arguments": `"Berlin"}`,
				},
			}},
		},
		"finish_reason": "tool_calls",
	}})

	body := sseBody(t, chunk1, chunk2)

	var responses []*model.LLMResponse
	m.handleStream(context.Background(), body, func(r *model.LLMResponse, err error) bool {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		responses = append(responses, r)
		return true
	})

	if len(responses) == 0 {
		t.Fatal("got 0 responses")
	}

	final := responses[len(responses)-1]
	if final.Partial {
		t.Error("final response should not be partial")
	}

	var fc *genai.FunctionCall
	for _, p := range final.Content.Parts {
		if p.FunctionCall != nil {
			fc = p.FunctionCall
			break
		}
	}
	if fc == nil {
		t.Fatal("no function call part in final response")
	}
	if fc.Name != "get_weather" {
		t.Errorf("function name = %q, want get_weather", fc.Name)
	}
	if fc.ID != "call_42" {
		t.Errorf("function ID = %q, want call_42", fc.ID)
	}
	if city, ok := fc.Args["city"].(string); !ok || city != "Berlin" {
		t.Errorf("args[city] = %v, want Berlin", fc.Args["city"])
	}
}

func TestHandleStream_UsageMetadata(t *testing.T) {
	m := newTestSAPModel(t, "http://base", "http://auth")

	body := sseBody(t,
		orchChunk([]any{textDelta("hi")}),
		// final_result envelope carries usage
		map[string]any{
			"final_result": map[string]any{
				"choices": []any{finishDelta("stop")},
				"usage": map[string]any{
					"prompt_tokens":     float64(10),
					"completion_tokens": float64(5),
				},
			},
		},
	)

	var responses []*model.LLMResponse
	m.handleStream(context.Background(), body, func(r *model.LLMResponse, err error) bool {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		responses = append(responses, r)
		return true
	})

	final := responses[len(responses)-1]
	if final.UsageMetadata == nil {
		t.Fatal("expected UsageMetadata in final response, got nil")
	}
	if final.UsageMetadata.PromptTokenCount != 10 {
		t.Errorf("PromptTokenCount = %d, want 10", final.UsageMetadata.PromptTokenCount)
	}
	if final.UsageMetadata.CandidatesTokenCount != 5 {
		t.Errorf("CandidatesTokenCount = %d, want 5", final.UsageMetadata.CandidatesTokenCount)
	}
}

func TestHandleStream_ErrorEvent(t *testing.T) {
	m := newTestSAPModel(t, "http://base", "http://auth")

	body := sseBody(t,
		map[string]any{"code": "500", "message": "internal error"},
	)

	var gotErr error
	m.handleStream(context.Background(), body, func(r *model.LLMResponse, err error) bool {
		if err != nil {
			gotErr = err
		}
		return true
	})

	if gotErr == nil {
		t.Error("handleStream() error = nil, want error for stream error event")
	}
}

func TestHandleStream_IgnoresMalformedLines(t *testing.T) {
	m := newTestSAPModel(t, "http://base", "http://auth")

	// Inject a non-JSON line between valid chunks; it must be silently skipped.
	var b strings.Builder
	b.WriteString("data: not-valid-json\n\n")
	raw, _ := json.Marshal(orchChunk([]any{textDelta("ok")}))
	b.WriteString("data: ")
	b.Write(raw)
	b.WriteString("\n\n")
	b.WriteString("data: [DONE]\n\n")

	var responses []*model.LLMResponse
	m.handleStream(context.Background(), strings.NewReader(b.String()), func(r *model.LLMResponse, err error) bool {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		responses = append(responses, r)
		return true
	})

	// Should still produce at least the partial + final for the valid chunk.
	if len(responses) < 2 {
		t.Errorf("got %d responses, want >= 2 (malformed line skipped)", len(responses))
	}
}

func TestHandleStream_ContextCancellation(t *testing.T) {
	m := newTestSAPModel(t, "http://base", "http://auth")

	// Build a stream with many chunks; cancel context after the first yield.
	chunks := make([]any, 20)
	for i := range chunks {
		chunks[i] = orchChunk([]any{textDelta("x")})
	}
	body := sseBody(t, chunks...)

	ctx, cancel := context.WithCancel(context.Background())
	count := 0
	m.handleStream(ctx, body, func(r *model.LLMResponse, err error) bool {
		count++
		cancel() // cancel after receiving the first chunk
		return true
	})

	// After cancellation the loop should stop; we should not receive all 20 partials.
	if count >= 20 {
		t.Errorf("received %d responses after cancel, expected early stop", count)
	}
}

func TestHandleStream_EmptyStream(t *testing.T) {
	m := newTestSAPModel(t, "http://base", "http://auth")

	body := sseBody(t) // only [DONE]

	var responses []*model.LLMResponse
	m.handleStream(context.Background(), body, func(r *model.LLMResponse, err error) bool {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		responses = append(responses, r)
		return true
	})

	// An empty stream yields one final TurnComplete response with no parts.
	if len(responses) != 1 {
		t.Fatalf("got %d responses, want 1 (empty final)", len(responses))
	}
	if responses[0].Partial || !responses[0].TurnComplete {
		t.Errorf("empty stream final: partial=%v turnComplete=%v, want false/true",
			responses[0].Partial, responses[0].TurnComplete)
	}
}

func TestGenaiToolsToOrchTools_WithParameters(t *testing.T) {
	// fd.Parameters (genai.Schema) path — used when ParametersJsonSchema is nil.
	tools := []*genai.Tool{{
		FunctionDeclarations: []*genai.FunctionDeclaration{{
			Name:        "scale_deployment",
			Description: "Scale a K8s deployment",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"replicas": {Type: genai.TypeInteger},
				},
			},
		}},
	}}
	got := genaiToolsToOrchTools(tools)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	fn := got[0]["function"].(map[string]any)
	if fn["name"] != "scale_deployment" {
		t.Errorf("name = %v, want scale_deployment", fn["name"])
	}
	params, ok := fn["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("parameters not a map: %v", fn["parameters"])
	}
	if params["type"] != "object" {
		t.Errorf("parameters.type = %v, want object", params["type"])
	}
}

func TestHandleStream_FinishReasonAtChoiceLevel(t *testing.T) {
	// finish_reason sits at the choice top level (not inside delta),
	// which is what SAP sends in the final chunk (sapaicore_adk.go:386).
	m := newTestSAPModel(t, "http://base", "http://auth")

	chunk := orchChunk([]any{map[string]any{
		"delta":         map[string]any{"content": "done"},
		"finish_reason": "length", // top-level, not inside delta
	}})
	body := sseBody(t, chunk)

	var responses []*model.LLMResponse
	m.handleStream(context.Background(), body, func(r *model.LLMResponse, err error) bool {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		responses = append(responses, r)
		return true
	})

	final := responses[len(responses)-1]
	if final.FinishReason != openAIFinishReasonToGenai("length") {
		t.Errorf("FinishReason = %v, want MAX_TOKENS (length)", final.FinishReason)
	}
}

// ---- helpers ----

func jsonReader(b []byte) *strings.Reader {
	return strings.NewReader(string(b))
}
