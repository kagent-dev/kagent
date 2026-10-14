//go:build linux

package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/harness/byo/dsh/internal/config"
	"github.com/kagent-dev/kagent/go/harness/runtime"
)

// This test uses the published DSH executable and a local OpenAI-compatible endpoint.
// It needs no provider credentials and checks conversation recovery after process exit.
func TestRealDSHResumesConversation(t *testing.T) {
	executable := os.Getenv("DSH_TEST_BIN")
	if executable == "" {
		t.Skip("set DSH_TEST_BIN to the pinned DSH executable")
	}
	var mu sync.Mutex
	var requests []json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var body json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}
		mu.Lock()
		requests = append(requests, body)
		index := len(requests)
		mu.Unlock()
		text := "first result"
		if index == 2 {
			text = "second result"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, choice := range []any{
			map[string]any{"delta": map[string]string{"role": "assistant", "content": text}, "index": 0, "finish_reason": nil},
			map[string]any{"delta": map[string]any{}, "index": 0, "finish_reason": "stop"},
		} {
			data, _ := json.Marshal(map[string]any{"id": "chat-test", "object": "chat.completion.chunk", "model": "mock", "choices": []any{choice}})
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	data, err := json.Marshal(struct {
		Model struct {
			Type    string `json:"type"`
			Model   string `json:"model"`
			BaseURL string `json:"base_url"`
		} `json:"model"`
		Instruction string `json:"instruction"`
		Runtime     struct {
			ControlPlaneURL string `json:"control_plane_url"`
		} `json:"runtime"`
	}{Model: struct {
		Type    string `json:"type"`
		Model   string `json:"model"`
		BaseURL string `json:"base_url"`
	}{Type: "openai", Model: "mock", BaseURL: server.URL + "/v1"}, Instruction: "Keep answers short.", Runtime: struct {
		ControlPlaneURL string `json:"control_plane_url"`
	}{ControlPlaneURL: "http://controller:8083"}})
	require.NoError(t, err)
	cfg, err := config.Parse(data)
	require.NoError(t, err)
	runner, err := New(cfg, executable, t.TempDir(), append(os.Environ(), "OPENAI_API_KEY=mock"))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	require.NoError(t, runner.Validate(ctx))
	first := &recordingSink{}
	out, err := runner.Run(ctx, runtime.Turn{Prompt: "Remember the number 27."}, first)
	require.NoError(t, err)
	require.Nil(t, out.Failure)
	require.Nil(t, out.Pending)
	require.Equal(t, "first result", strings.Join(first.texts, ""))
	require.NotEmpty(t, first.id)
	second := &recordingSink{}
	out, err = runner.Run(ctx, runtime.Turn{Prompt: "Continue the previous conversation.", ContinuationID: first.id}, second)
	require.NoError(t, err)
	require.Nil(t, out.Failure)
	require.Equal(t, first.id, second.id)
	require.Equal(t, "second result", strings.Join(second.texts, ""))
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, requests, 2)
	require.Contains(t, string(requests[0]), "Keep answers short.")
	require.Contains(t, string(requests[1]), "first result")
	require.Contains(t, string(requests[1]), "Remember the number 27.")
}

func TestRealDSHCancelsAndResumes(t *testing.T) {
	executable := os.Getenv("DSH_TEST_BIN")
	if executable == "" {
		t.Skip("set DSH_TEST_BIN to the pinned DSH executable")
	}
	started := make(chan struct{})
	cleanup := make(chan struct{})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			return
		}
		if requests.Add(1) == 1 {
			close(started)
			select {
			case <-r.Context().Done():
			case <-cleanup:
			}
			return
		}
		writeCompletion(w, map[string]string{"role": "assistant", "content": "recovered"}, "stop")
	}))
	defer func() { close(cleanup); server.Close() }()
	cfg, err := config.Parse([]byte(fmt.Sprintf(`{"model":{"type":"openai","model":"mock","base_url":%q},"runtime":{"control_plane_url":"http://controller:8083"}}`, server.URL+"/v1")))
	require.NoError(t, err)
	runner, err := New(cfg, executable, t.TempDir(), append(os.Environ(), "OPENAI_API_KEY=mock"))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sink := &recordingSink{}
	done := make(chan error, 1)
	go func() { _, err := runner.Run(ctx, runtime.Turn{Prompt: "Start a long operation."}, sink); done <- err }()
	select {
	case <-started:
		cancel()
	case <-time.After(30 * time.Second):
		t.Fatal("DSH did not start the model request")
	}
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(15 * time.Second):
		t.Fatal("DSH cancellation did not settle")
	}
	require.NotEmpty(t, sink.id)
	resumeCtx, resumeCancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer resumeCancel()
	resumed := &recordingSink{}
	out, err := runner.Run(resumeCtx, runtime.Turn{Prompt: "Continue after cancellation.", ContinuationID: sink.id}, resumed)
	require.NoError(t, err)
	require.Nil(t, out.Failure)
	require.Equal(t, "recovered", strings.Join(resumed.texts, ""))
	require.Equal(t, sink.id, resumed.id)
	require.Equal(t, int32(2), requests.Load())
}
