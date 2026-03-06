package temporal

import (
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/api/adk"
)

func TestFromRuntimeConfig_Nil(t *testing.T) {
	cfg := FromRuntimeConfig(nil)
	def := DefaultTemporalConfig()
	if cfg.Namespace != def.Namespace {
		t.Errorf("Expected default namespace %s, got %s", def.Namespace, cfg.Namespace)
	}
	if cfg.WorkflowTimeout != def.WorkflowTimeout {
		t.Errorf("Expected default timeout %v, got %v", def.WorkflowTimeout, cfg.WorkflowTimeout)
	}
}

func TestFromRuntimeConfig_AllFields(t *testing.T) {
	rc := &adk.TemporalRuntimeConfig{
		Enabled:         true,
		HostAddr:        "temporal:7233",
		Namespace:       "prod",
		TaskQueue:       "agent-myagent",
		NATSAddr:        "nats://nats:4222",
		WorkflowTimeout: "24h",
		LLMMaxAttempts:  10,
		ToolMaxAttempts: 5,
	}
	cfg := FromRuntimeConfig(rc)

	if !cfg.Enabled {
		t.Error("Expected enabled=true")
	}
	if cfg.HostAddr != "temporal:7233" {
		t.Errorf("Expected hostAddr temporal:7233, got %s", cfg.HostAddr)
	}
	if cfg.Namespace != "prod" {
		t.Errorf("Expected namespace prod, got %s", cfg.Namespace)
	}
	if cfg.TaskQueue != "agent-myagent" {
		t.Errorf("Expected taskQueue agent-myagent, got %s", cfg.TaskQueue)
	}
	if cfg.NATSAddr != "nats://nats:4222" {
		t.Errorf("Expected natsAddr nats://nats:4222, got %s", cfg.NATSAddr)
	}
	if cfg.WorkflowTimeout != 24*time.Hour {
		t.Errorf("Expected 24h timeout, got %v", cfg.WorkflowTimeout)
	}
	if cfg.LLMMaxAttempts != 10 {
		t.Errorf("Expected 10 LLM attempts, got %d", cfg.LLMMaxAttempts)
	}
	if cfg.ToolMaxAttempts != 5 {
		t.Errorf("Expected 5 tool attempts, got %d", cfg.ToolMaxAttempts)
	}
}

func TestFromRuntimeConfig_Defaults(t *testing.T) {
	rc := &adk.TemporalRuntimeConfig{Enabled: true}
	cfg := FromRuntimeConfig(rc)
	def := DefaultTemporalConfig()

	if cfg.Namespace != def.Namespace {
		t.Errorf("Expected default namespace %s, got %s", def.Namespace, cfg.Namespace)
	}
	if cfg.WorkflowTimeout != def.WorkflowTimeout {
		t.Errorf("Expected default timeout %v, got %v", def.WorkflowTimeout, cfg.WorkflowTimeout)
	}
	if cfg.LLMMaxAttempts != def.LLMMaxAttempts {
		t.Errorf("Expected default LLM attempts %d, got %d", def.LLMMaxAttempts, cfg.LLMMaxAttempts)
	}
}

func TestFromRuntimeConfig_InvalidDuration(t *testing.T) {
	rc := &adk.TemporalRuntimeConfig{
		Enabled:         true,
		WorkflowTimeout: "invalid",
	}
	cfg := FromRuntimeConfig(rc)
	def := DefaultTemporalConfig()

	if cfg.WorkflowTimeout != def.WorkflowTimeout {
		t.Errorf("Expected default timeout for invalid duration, got %v", cfg.WorkflowTimeout)
	}
}
