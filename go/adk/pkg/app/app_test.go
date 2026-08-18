package app

import (
	"context"
	"iter"
	"slices"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
)

// fakeExecutor implements a2asrv.AgentExecutor for testing.
type fakeExecutor struct{}

func (f *fakeExecutor) Execute(_ context.Context, _ *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {}
}

func (f *fakeExecutor) Cancel(_ context.Context, _ *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {}
}

var _ a2asrv.AgentExecutor = (*fakeExecutor)(nil)

func TestNew_NilExecutor(t *testing.T) {
	_, err := New(AppConfig{
		AgentCard: a2atype.AgentCard{Name: "test"},
	}, nil)
	if err == nil {
		t.Fatal("expected error for nil executor, got nil")
	}
}

func TestNew_Success(t *testing.T) {
	app, err := New(AppConfig{
		AgentCard: a2atype.AgentCard{Name: "test-agent"},
		Port:      "0",
	}, &fakeExecutor{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if app == nil {
		t.Fatal("expected non-nil app")
	}
	if app.SessionService() != nil {
		t.Error("expected nil session service when KAgentGRPCURL is empty")
	}
}

func TestNew_WithKAgentGRPCURL(t *testing.T) {
	t.Setenv("KAGENT_GRPC_URL", "")

	app, err := New(AppConfig{
		AgentCard:     a2atype.AgentCard{Name: "test-agent"},
		Port:          "0",
		KAgentGRPCURL: "localhost:9999",
	}, &fakeExecutor{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if app.SessionService() == nil {
		t.Error("expected non-nil session service when KAgentGRPCURL is set")
	}
	app.stop()
}

func TestApplyDefaults_Port(t *testing.T) {
	t.Setenv("PORT", "")
	cfg := applyDefaults(AppConfig{})
	if cfg.Port != defaultPort {
		t.Errorf("expected port %q, got %q", defaultPort, cfg.Port)
	}
}

func TestApplyDefaults_PortFromEnv(t *testing.T) {
	t.Setenv("PORT", "9090")
	cfg := applyDefaults(AppConfig{})
	if cfg.Port != "9090" {
		t.Errorf("expected port %q, got %q", "9090", cfg.Port)
	}
}

func TestApplyDefaults_PortExplicit(t *testing.T) {
	t.Setenv("PORT", "9090")
	cfg := applyDefaults(AppConfig{Port: "3000"})
	if cfg.Port != "3000" {
		t.Errorf("expected port %q, got %q", "3000", cfg.Port)
	}
}

func TestApplyDefaults_ShutdownTimeout(t *testing.T) {
	cfg := applyDefaults(AppConfig{})
	if cfg.ShutdownTimeout != defaultShutdownTimeout {
		t.Errorf("expected shutdown timeout %v, got %v", defaultShutdownTimeout, cfg.ShutdownTimeout)
	}
}

func TestApplyDefaults_ShutdownTimeoutExplicit(t *testing.T) {
	cfg := applyDefaults(AppConfig{ShutdownTimeout: 10 * time.Second})
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf("expected shutdown timeout %v, got %v", 10*time.Second, cfg.ShutdownTimeout)
	}
}

func TestApplyDefaults_KAgentGRPCURLFromEnv(t *testing.T) {
	t.Setenv("KAGENT_GRPC_URL", "env-url:8084")
	cfg := applyDefaults(AppConfig{})
	if cfg.KAgentGRPCURL != "env-url:8084" {
		t.Errorf("expected KAgentGRPCURL from env, got %q", cfg.KAgentGRPCURL)
	}
}

func TestApplyDefaults_KAgentGRPCURLExplicit(t *testing.T) {
	t.Setenv("KAGENT_GRPC_URL", "env-url:8084")
	cfg := applyDefaults(AppConfig{KAgentGRPCURL: "explicit:8084"})
	if cfg.KAgentGRPCURL != "explicit:8084" {
		t.Errorf("expected explicit KAgentGRPCURL, got %q", cfg.KAgentGRPCURL)
	}
}

func TestApplyDefaults_Logger(t *testing.T) {
	cfg := applyDefaults(AppConfig{})
	if cfg.Logger.GetSink() == nil {
		t.Error("expected default logger to be created")
	}
}

func TestBuildAppName_FromEnv(t *testing.T) {
	t.Setenv("KAGENT_NAME", "my-agent")
	t.Setenv("KAGENT_NAMESPACE", "my-ns")
	name := buildAppName(&a2atype.AgentCard{Name: "card-name"})
	if name != "my_ns__NS__my_agent" {
		t.Errorf("expected %q, got %q", "my_ns__NS__my_agent", name)
	}
}

func TestBuildAppName_FromAgentCard(t *testing.T) {
	t.Setenv("KAGENT_NAME", "")
	t.Setenv("KAGENT_NAMESPACE", "")
	name := buildAppName(&a2atype.AgentCard{Name: "card-name"})
	if name != "card-name" {
		t.Errorf("expected %q, got %q", "card-name", name)
	}
}

func TestBuildAppName_Default(t *testing.T) {
	t.Setenv("KAGENT_NAME", "")
	t.Setenv("KAGENT_NAMESPACE", "")
	name := buildAppName(&a2atype.AgentCard{})
	if name != defaultAppName {
		t.Errorf("expected %q, got %q", defaultAppName, name)
	}
}

func TestBuildAgentCard_DeclaresHITLWithoutAgent(t *testing.T) {
	card := buildAgentCard(AppConfig{AgentCard: a2atype.AgentCard{Name: "byo-agent"}})

	if !slices.ContainsFunc(card.Capabilities.Extensions, func(extension a2atype.AgentExtension) bool {
		return extension.URI == a2a.HITLExtensionURI
	}) {
		t.Fatalf("extensions = %#v, want the HITL extension", card.Capabilities.Extensions)
	}
}

func TestBuildAgentCard_DeclaresHITLWithAgent(t *testing.T) {
	agent, err := adkagent.New(adkagent.Config{
		Name:        "adk_agent",
		Description: "an ADK agent",
		Run: func(adkagent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {}
		},
	})
	if err != nil {
		t.Fatalf("adkagent.New: %v", err)
	}

	card := buildAgentCard(AppConfig{AgentCard: a2atype.AgentCard{Name: "adk-agent"}, Agent: agent})

	if !slices.ContainsFunc(card.Capabilities.Extensions, func(extension a2atype.AgentExtension) bool {
		return extension.URI == a2a.HITLExtensionURI
	}) {
		t.Fatalf("extensions = %#v, want the HITL extension", card.Capabilities.Extensions)
	}
	if card.Description != "an ADK agent" {
		t.Errorf("description = %q, want the agent description", card.Description)
	}
}

func TestBuildAgentCard_LeavesCallerCardUntouched(t *testing.T) {
	cfg := AppConfig{AgentCard: a2atype.AgentCard{Name: "byo-agent"}}

	buildAgentCard(cfg)

	if len(cfg.AgentCard.Capabilities.Extensions) != 0 {
		t.Errorf("caller extensions = %#v, want the caller's card untouched", cfg.AgentCard.Capabilities.Extensions)
	}
}
