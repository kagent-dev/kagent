package app

import (
	"context"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
)

// fakeExecutor implements a2asrv.AgentExecutor for testing.
type fakeExecutor struct{}

func (f *fakeExecutor) Execute(_ context.Context, _ *a2asrv.RequestContext, _ eventqueue.Queue) error {
	return nil
}

func (f *fakeExecutor) Cancel(_ context.Context, _ *a2asrv.RequestContext, _ eventqueue.Queue) error {
	return nil
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
		t.Error("expected nil session service when KAgentURL is empty")
	}
}

func TestNew_WithKAgentURL(t *testing.T) {
	t.Setenv("KAGENT_URL", "")

	app, err := New(AppConfig{
		AgentCard: a2atype.AgentCard{Name: "test-agent"},
		Port:      "0",
		KAgentURL: "http://localhost:9999",
	}, &fakeExecutor{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if app.SessionService() == nil {
		t.Error("expected non-nil session service when KAgentURL is set")
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

func TestApplyDefaults_KAgentURLFromEnv(t *testing.T) {
	t.Setenv("KAGENT_URL", "http://env-url:8083")
	cfg := applyDefaults(AppConfig{})
	if cfg.KAgentURL != "http://env-url:8083" {
		t.Errorf("expected KAgentURL from env, got %q", cfg.KAgentURL)
	}
}

func TestApplyDefaults_KAgentURLExplicit(t *testing.T) {
	t.Setenv("KAGENT_URL", "http://env-url:8083")
	cfg := applyDefaults(AppConfig{KAgentURL: "http://explicit:8083"})
	if cfg.KAgentURL != "http://explicit:8083" {
		t.Errorf("expected explicit KAgentURL, got %q", cfg.KAgentURL)
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
