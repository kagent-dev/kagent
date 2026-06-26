package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/api/adk"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

func TestBuildReliabilityPlugins(t *testing.T) {
	tests := []struct {
		name      string
		config    *adk.ReliabilityConfig
		wantNames []string
	}{
		{
			name:      "nil config",
			config:    nil,
			wantNames: nil,
		},
		{
			name:      "empty config",
			config:    &adk.ReliabilityConfig{},
			wantNames: nil,
		},
		{
			name:      "debug logging only",
			config:    &adk.ReliabilityConfig{DebugLogging: new(true)},
			wantNames: []string{"kagent_debug_logging"},
		},
		{
			name:      "debug logging false",
			config:    &adk.ReliabilityConfig{DebugLogging: new(false)},
			wantNames: nil,
		},
		{
			name:      "tool retries only",
			config:    &adk.ReliabilityConfig{ToolRetries: new(3)},
			wantNames: []string{"RetryAndReflectPlugin"},
		},
		{
			name:      "max llm calls only",
			config:    &adk.ReliabilityConfig{MaxLLMCalls: new(10)},
			wantNames: []string{"kagent_max_llm_calls"},
		},
		{
			name: "all enabled",
			config: &adk.ReliabilityConfig{
				ToolRetries:  new(2),
				MaxLLMCalls:  new(50),
				DebugLogging: new(true),
			},
			wantNames: []string{"kagent_debug_logging", "RetryAndReflectPlugin", "kagent_max_llm_calls"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugins, err := buildReliabilityPlugins(tt.config, logr.Discard())
			if err != nil {
				t.Fatalf("buildReliabilityPlugins() error = %v", err)
			}
			if len(plugins) != len(tt.wantNames) {
				t.Fatalf("got %d plugins, want %d", len(plugins), len(tt.wantNames))
			}
			for i, want := range tt.wantNames {
				if got := plugins[i].Name(); got != want {
					t.Errorf("plugin[%d].Name() = %q, want %q", i, got, want)
				}
			}
		})
	}
}

// fakeCallbackContext is a minimal agent.CallbackContext for testing.
type fakeCallbackContext struct {
	context.Context
	invocationID string
}

func (f *fakeCallbackContext) UserContent() *genai.Content          { return nil }
func (f *fakeCallbackContext) InvocationID() string                 { return f.invocationID }
func (f *fakeCallbackContext) AgentName() string                    { return "test-agent" }
func (f *fakeCallbackContext) ReadonlyState() session.ReadonlyState { return nil }
func (f *fakeCallbackContext) UserID() string                       { return "user" }
func (f *fakeCallbackContext) AppName() string                      { return "app" }
func (f *fakeCallbackContext) SessionID() string                    { return "session" }
func (f *fakeCallbackContext) Branch() string                       { return "" }
func (f *fakeCallbackContext) Artifacts() agent.Artifacts           { return nil }
func (f *fakeCallbackContext) State() session.State                 { return nil }

func TestMaxLLMCallsPlugin(t *testing.T) {
	p, err := newMaxLLMCallsPlugin(2)
	if err != nil {
		t.Fatalf("newMaxLLMCallsPlugin() error = %v", err)
	}
	cb := p.BeforeModelCallback()
	if cb == nil {
		t.Fatal("BeforeModelCallback is nil")
	}

	ctxA := &fakeCallbackContext{Context: context.Background(), invocationID: "inv-a"}
	ctxB := &fakeCallbackContext{Context: context.Background(), invocationID: "inv-b"}

	// First two calls within the limit succeed.
	for i := range 2 {
		if _, err := cb(ctxA, nil); err != nil {
			t.Fatalf("call %d: unexpected error: %v", i+1, err)
		}
	}

	// Third call exceeds the limit.
	if _, err := cb(ctxA, nil); err == nil {
		t.Fatal("expected error after exceeding limit, got nil")
	} else if !strings.Contains(err.Error(), "limit of 2 model calls") {
		t.Errorf("unexpected error message: %v", err)
	}

	// A different invocation has its own counter.
	if _, err := cb(ctxB, nil); err != nil {
		t.Fatalf("different invocation should not be limited: %v", err)
	}
}
