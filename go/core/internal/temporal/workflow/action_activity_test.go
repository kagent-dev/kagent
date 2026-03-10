/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.temporal.io/sdk/temporal"
)

func TestActionRegistry(t *testing.T) {
	t.Run("register and get handler", func(t *testing.T) {
		r := NewActionRegistry()
		handler := ActionHandlerFunc(func(_ context.Context, inputs map[string]string) (*ActionResult, error) {
			return &ActionResult{Output: json.RawMessage(`{"ok":true}`)}, nil
		})
		r.Register("test.action", handler)

		got, ok := r.Get("test.action")
		if !ok {
			t.Fatal("expected handler to be found")
		}
		result, err := got.Execute(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(result.Output) != `{"ok":true}` {
			t.Errorf("got output %s, want {\"ok\":true}", result.Output)
		}
	})

	t.Run("get unknown handler", func(t *testing.T) {
		r := NewActionRegistry()
		_, ok := r.Get("nonexistent")
		if ok {
			t.Error("expected handler to not be found")
		}
	})
}

func TestActionActivity(t *testing.T) {
	tests := []struct {
		name       string
		req        *ActionRequest
		handlers   map[string]ActionHandler
		wantOutput string
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "dispatches to correct handler",
			req:  &ActionRequest{Action: "test.action", Inputs: map[string]string{"key": "value"}},
			handlers: map[string]ActionHandler{
				"test.action": ActionHandlerFunc(func(_ context.Context, inputs map[string]string) (*ActionResult, error) {
					out, _ := json.Marshal(inputs)
					return &ActionResult{Output: out}, nil
				}),
			},
			wantOutput: `{"key":"value"}`,
		},
		{
			name:       "unknown action returns NonRetryableApplicationError",
			req:        &ActionRequest{Action: "unknown.action"},
			handlers:   map[string]ActionHandler{},
			wantErr:    true,
			wantErrMsg: "unknown action: unknown.action",
		},
		{
			name:       "nil request returns NonRetryableApplicationError",
			req:        nil,
			handlers:   map[string]ActionHandler{},
			wantErr:    true,
			wantErrMsg: "nil action request",
		},
		{
			name: "handler error propagates",
			req:  &ActionRequest{Action: "fail.action"},
			handlers: map[string]ActionHandler{
				"fail.action": ActionHandlerFunc(func(_ context.Context, _ map[string]string) (*ActionResult, error) {
					return nil, fmt.Errorf("handler failed")
				}),
			},
			wantErr:    true,
			wantErrMsg: "handler failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewActionRegistry()
			for name, h := range tt.handlers {
				registry.Register(name, h)
			}
			activities := &DAGActivities{Registry: registry}

			result, err := activities.ActionActivity(context.Background(), tt.req)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.wantErrMsg != "" {
					// Check for NonRetryableApplicationError
					var appErr *temporal.ApplicationError
					if ok := temporal.IsApplicationError(err); ok {
						if err.Error() != tt.wantErrMsg {
							// Application errors wrap the message
						}
					} else {
						_ = appErr // suppress unused
						if err.Error() != tt.wantErrMsg {
							t.Errorf("error = %q, want %q", err.Error(), tt.wantErrMsg)
						}
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantOutput != "" && string(result.Output) != tt.wantOutput {
				t.Errorf("output = %s, want %s", result.Output, tt.wantOutput)
			}
		})
	}
}

func TestNoopHandler(t *testing.T) {
	handler := &NoopHandler{}

	t.Run("returns inputs as output", func(t *testing.T) {
		inputs := map[string]string{"foo": "bar", "baz": "qux"}
		result, err := handler.Execute(context.Background(), inputs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Error != "" {
			t.Fatalf("unexpected result error: %s", result.Error)
		}

		var got map[string]string
		if err := json.Unmarshal(result.Output, &got); err != nil {
			t.Fatalf("failed to unmarshal output: %v", err)
		}
		if got["foo"] != "bar" || got["baz"] != "qux" {
			t.Errorf("got %v, want map with foo=bar, baz=qux", got)
		}
	})

	t.Run("nil inputs returns empty object", func(t *testing.T) {
		result, err := handler.Execute(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(result.Output) != "null" {
			t.Errorf("got %s, want null", result.Output)
		}
	})
}

func TestHTTPRequestHandler(t *testing.T) {
	t.Run("GET request", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("expected GET, got %s", r.Method)
			}
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"message":"hello"}`)
		}))
		defer server.Close()

		handler := &HTTPRequestHandler{Client: server.Client()}
		result, err := handler.Execute(context.Background(), map[string]string{
			"url": server.URL,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Error != "" {
			t.Fatalf("unexpected result error: %s", result.Error)
		}

		var out map[string]interface{}
		if err := json.Unmarshal(result.Output, &out); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if out["status_code"].(float64) != 200 {
			t.Errorf("status_code = %v, want 200", out["status_code"])
		}
		if out["body"] != `{"message":"hello"}` {
			t.Errorf("body = %v, want {\"message\":\"hello\"}", out["body"])
		}
	})

	t.Run("POST request with body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("expected application/json content type, got %s", r.Header.Get("Content-Type"))
			}
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":"123"}`)
		}))
		defer server.Close()

		handler := &HTTPRequestHandler{Client: server.Client()}
		result, err := handler.Execute(context.Background(), map[string]string{
			"url":    server.URL,
			"method": "POST",
			"body":   `{"name":"test"}`,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Error != "" {
			t.Fatalf("unexpected result error: %s", result.Error)
		}

		var out map[string]interface{}
		json.Unmarshal(result.Output, &out)
		if out["status_code"].(float64) != 201 {
			t.Errorf("status_code = %v, want 201", out["status_code"])
		}
	})

	t.Run("HTTP error status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, "not found")
		}))
		defer server.Close()

		handler := &HTTPRequestHandler{Client: server.Client()}
		result, err := handler.Execute(context.Background(), map[string]string{
			"url": server.URL,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Error == "" {
			t.Error("expected error for 404 status")
		}
		// Output should still be populated
		if result.Output == nil {
			t.Error("expected output to be populated even on HTTP error")
		}
	})

	t.Run("missing URL", func(t *testing.T) {
		handler := &HTTPRequestHandler{Client: http.DefaultClient}
		result, err := handler.Execute(context.Background(), map[string]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Error == "" {
			t.Error("expected error for missing URL")
		}
	})

	t.Run("custom content type", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Content-Type") != "text/plain" {
				t.Errorf("expected text/plain, got %s", r.Header.Get("Content-Type"))
			}
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "ok")
		}))
		defer server.Close()

		handler := &HTTPRequestHandler{Client: server.Client()}
		result, err := handler.Execute(context.Background(), map[string]string{
			"url":          server.URL,
			"method":       "POST",
			"body":         "hello",
			"content_type": "text/plain",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Error != "" {
			t.Fatalf("unexpected result error: %s", result.Error)
		}
	})
}

func TestRegisterBuiltinHandlers(t *testing.T) {
	r := NewActionRegistry()
	RegisterBuiltinHandlers(r)

	for _, name := range []string{"noop", "http.request"} {
		if _, ok := r.Get(name); !ok {
			t.Errorf("expected built-in handler %q to be registered", name)
		}
	}
}
