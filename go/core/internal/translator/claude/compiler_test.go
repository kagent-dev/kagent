package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	claudeconfig "github.com/kagent-dev/kagent/go/harness/claude/config"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

const credentialValue = "credential-must-not-be-serialized"

func TestCompileProviderCredentials(t *testing.T) {
	tests := []struct {
		name       string
		model      v1alpha3.ModelConfigSpec
		secretData map[string][]byte
		wantEnv    map[string]string
		wantEgress []string
		wantErr    string
	}{
		{
			name: "caller token passthrough is unsupported",
			model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropic, Model: "model", Anthropic: &v1alpha3.AnthropicConfig{},
				Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{TokenPassthrough: &v1alpha3.TokenPassthrough{}}}}},
			wantErr: "does not support tokenPassthrough",
		},
		{
			name:       "Anthropic",
			model:      v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropic, Model: "claude-sonnet-4-5", Headers: []v1alpha3.Header{{Name: "X-API-Key", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "api-key"}}}}},
			secretData: map[string][]byte{"api-key": []byte(credentialValue)},
			wantEnv:    map[string]string{claudeconfig.AnthropicAPIKeyEnvName: translator.CredentialPlaceholder},
			wantEgress: []string{"http://kagent-controller.kagent:8083", "https://api.anthropic.com:443"},
		},
		{
			name: "Anthropic gateway",
			model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropic, Model: "claude-sonnet-4-5",

				// cacheTTL is defaulted to "5m" by the CRD whenever the anthropic block is set.
				Anthropic: &v1alpha3.AnthropicConfig{BaseURL: "http://host.docker.internal:8090/anthropic", CacheTTL: "5m"}, Headers: []v1alpha3.Header{{Name: "X-API-Key", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "api-key"}}}},
			},
			secretData: map[string][]byte{"api-key": []byte(credentialValue)},
			wantEnv: map[string]string{claudeconfig.AnthropicAPIKeyEnvName: translator.CredentialPlaceholder,
				claudeconfig.AnthropicBaseURLEnvName: "http://host.docker.internal:8090/anthropic"},
			wantEgress: []string{"http://host.docker.internal:8090", "http://kagent-controller.kagent:8083"},
		},
		{
			name: "Bedrock API key",
			model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderBedrock, Model: "us.anthropic.claude-sonnet-4-5-20250929-v1:0",
				Bedrock: &v1alpha3.BedrockConfig{Region: "us-west-2"}, Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "AWS_BEARER_TOKEN_BEDROCK"}}}}},
			secretData: map[string][]byte{claudeconfig.AWSBedrockTokenEnvName: []byte(credentialValue)},
			wantEnv:    map[string]string{claudeconfig.UseBedrockEnvName: "1", claudeconfig.AWSRegionEnvName: "us-west-2", claudeconfig.AWSBedrockTokenEnvName: translator.CredentialPlaceholder},
			wantEgress: []string{"http://kagent-controller.kagent:8083", "https://bedrock-runtime.us-west-2.amazonaws.com:443"},
		},
		{
			name: "Anthropic Vertex AI",
			model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropicVertexAI, Model: "claude-sonnet-4-5@20250929",

				AnthropicVertexAI: &v1alpha3.AnthropicVertexAIConfig{BaseVertexAIConfig: v1alpha3.BaseVertexAIConfig{ProjectID: "project", Location: "us-east5"}}, Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "credentials.json"}}}},
			},
			secretData: map[string][]byte{"credentials.json": []byte(`{"type":"service_account","project_id":"project","token_uri":"https://oauth2.googleapis.com/token","private_key":"` + credentialValue + `"}`)},
			wantErr:    "gateway header authentication does not support",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, reader := testInput(t, tt.model, tt.secretData)
			revision, err := NewCompiler(krt.TestingDummyContext{}, reader, testRuntimeDefaults()).Compile(context.Background(), input)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected unsupported credential error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var config claudeconfig.Config
			if err := json.Unmarshal(revision.ConfigJSON, &config); err != nil {
				t.Fatal(err)
			}
			if config.Model != tt.model.Model || config.AppendSystemPrompt != "help carefully" || config.ExpectedClaudeVersion != claudeconfig.PinnedClaudeVersion {
				t.Fatalf("compiled config = %#v", config)
			}
			gotEnvironment := map[string]string{}
			for _, variable := range revision.Environment {
				if variable.ValueFrom != nil {
					t.Fatalf("unresolved environment variable = %#v", variable)
				}
				gotEnvironment[variable.Name] = variable.Value
			}
			for name, value := range tt.wantEnv {
				if gotEnvironment[name] != value {
					t.Errorf("environment[%s] = %q, want %q", name, gotEnvironment[name], value)
				}
			}
			if gotEnvironment[claudeconfig.SandboxEnvName] != "1" {
				t.Errorf("environment[%s] = %q, want %q", claudeconfig.SandboxEnvName, gotEnvironment[claudeconfig.SandboxEnvName], "1")
			}
			if !reflect.DeepEqual(revision.EgressDestinations, tt.wantEgress) {
				t.Errorf("egress = %v", revision.EgressDestinations)
			}
			if bytes.Contains(revision.ConfigJSON, []byte(credentialValue)) || bytes.Contains(revision.Provenance, []byte(credentialValue)) {
				t.Fatal("compiled config or provenance contains credential material")
			}
			if bytes.Contains(revision.Provenance, []byte(`"kind":"Secret"`)) {
				t.Fatalf("provenance contains gateway-managed Secret: %s", revision.Provenance)
			}

			again, err := NewCompiler(krt.TestingDummyContext{}, reader, testRuntimeDefaults()).Compile(context.Background(), input)
			if err != nil || !reflect.DeepEqual(revision, again) {
				t.Fatalf("compilation is not deterministic: %v", err)
			}
		})
	}
}

func TestCompileRejectsUnsupportedConfiguration(t *testing.T) {
	tests := []struct {
		name  string
		model v1alpha3.ModelConfigSpec
	}{
		{name: "provider", model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt"}},
		{name: "Anthropic options", model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropic, Model: "claude", Anthropic: &v1alpha3.AnthropicConfig{Temperature: "0.5"}}},
		{name: "Anthropic prompt caching", model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropic, Model: "claude", Anthropic: &v1alpha3.AnthropicConfig{PromptCaching: true, CacheTTL: "5m"}, Headers: []v1alpha3.Header{{Name: "X-API-Key", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "api-key"}}}}}},
		{name: "Anthropic cache TTL", model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropic, Model: "claude", Anthropic: &v1alpha3.AnthropicConfig{CacheTTL: "1h"}, Headers: []v1alpha3.Header{{Name: "X-API-Key", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "api-key"}}}}}},
		{name: "Anthropic relative base URL", model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropic, Model: "claude", Anthropic: &v1alpha3.AnthropicConfig{BaseURL: "/v1"}, Headers: []v1alpha3.Header{{Name: "X-API-Key", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "api-key"}}}}}},
		{name: "Anthropic base URL credentials", model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropic, Model: "claude", Anthropic: &v1alpha3.AnthropicConfig{BaseURL: "https://user:password@example.com"}, Headers: []v1alpha3.Header{{Name: "X-API-Key", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "api-key"}}}}}},
		{name: "Bedrock options", model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderBedrock, Model: "claude", Bedrock: &v1alpha3.BedrockConfig{Region: "us-east-1", PromptCaching: true}, Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "AWS_BEARER_TOKEN_BEDROCK"}}}}}},
		{name: "Vertex options", model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropicVertexAI, Model: "claude", AnthropicVertexAI: &v1alpha3.AnthropicVertexAIConfig{BaseVertexAIConfig: v1alpha3.BaseVertexAIConfig{ProjectID: "project", Location: "global", Temperature: "0.5"}}, Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "credentials.json"}}}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, reader := testInput(t, tt.model, map[string][]byte{"api-key": []byte("secret"), claudeconfig.AWSAccessKeyEnvName: []byte("access"), claudeconfig.AWSSecretKeyEnvName: []byte("secret"), "credentials.json": []byte(`{"type":"service_account"}`)})
			_, err := NewCompiler(krt.TestingDummyContext{}, reader, testRuntimeDefaults()).Compile(context.Background(), input)
			var validation *translator.ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("Compile() error = %v, want validation error", err)
			}
		})
	}
}

func TestCompileRejectsProviderOwnedHarnessEnvironment(t *testing.T) {
	model := v1alpha3.ModelConfigSpec{
		Provider: v1alpha3.ModelProviderAnthropic, Model: "claude-sonnet-4-5", Headers: []v1alpha3.Header{{Name: "X-API-Key", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "api-key"}}}},
	}
	input, reader := testInput(t, model, map[string][]byte{"api-key": []byte("secret")})
	value := "http://mock.example.com"
	input.Harness.Spec.Env = []v1alpha3.RuntimeEnvVar{{Name: claudeconfig.AnthropicBaseURLEnvName, Value: value}}
	_, err := NewCompiler(krt.TestingDummyContext{}, reader, testRuntimeDefaults()).Compile(context.Background(), input)
	var validation *translator.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("Compile() error = %v, want validation error", err)
	}
}

func TestCompileAllowsRuntimeOTELEndpoint(t *testing.T) {
	model := v1alpha3.ModelConfigSpec{
		Provider: v1alpha3.ModelProviderAnthropic, Model: "claude-sonnet-4-5", Headers: []v1alpha3.Header{{Name: "X-API-Key", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "api-key"}}}},
	}
	input, reader := testInput(t, model, map[string][]byte{"api-key": []byte("secret")})
	value := "http://other-collector:4317"
	input.Harness.Spec.Env = []v1alpha3.RuntimeEnvVar{{Name: "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", Value: value}}

	result, err := NewCompiler(krt.TestingDummyContext{}, reader, testRuntimeDefaults()).Compile(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(result.EgressDestinations, value) {
		t.Fatalf("missing runtime telemetry egress: %v", result.EgressDestinations)
	}
}

func TestCompileAllowsRuntimeTelemetryOverrides(t *testing.T) {
	model := v1alpha3.ModelConfigSpec{
		Provider: v1alpha3.ModelProviderAnthropic, Model: "claude-sonnet-4-5", Headers: []v1alpha3.Header{{Name: "X-API-Key", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "api-key"}}}},
	}
	input, reader := testInput(t, model, map[string][]byte{"api-key": []byte("secret")})
	value := "department=engineering"
	input.Harness.Spec.Env = []v1alpha3.RuntimeEnvVar{{Name: "OTEL_RESOURCE_ATTRIBUTES", Value: value}}

	result, err := NewCompiler(krt.TestingDummyContext{}, reader, testRuntimeDefaults()).Compile(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(result.Environment, func(v corev1.EnvVar) bool { return v.Name == input.Harness.Spec.Env[0].Name && v.Value == value }) {
		t.Fatal("runtime override missing")
	}
}

func TestCompileRootSkillsAndPluginSelections(t *testing.T) {
	model := v1alpha3.ModelConfigSpec{
		Provider: v1alpha3.ModelProviderAnthropic, Model: "claude-sonnet-4-5", Headers: []v1alpha3.Header{{Name: "X-API-Key", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "api-key"}}}},
	}
	input, reader := testInput(t, model, map[string][]byte{"api-key": []byte("secret")})
	input.Root.Template.Spec.Skills = []v1alpha3.AgentTemplateSkill{{
		Name: "review", Source: v1alpha3.ArtifactSource{Git: &v1alpha3.GitArtifact{
			URL: "https://git.example.com/skills.git", Commit: strings.Repeat("a", 40),
		}},
	}}
	input.Root.Template.Spec.Plugins = []v1alpha3.PluginBundle{{
		Source: v1alpha3.ArtifactSource{OCI: "registry.example.com/team/plugin@sha256:" + strings.Repeat("b", 64)},
		Skills: []string{"deploy"},
	}}

	revision, err := NewCompiler(krt.TestingDummyContext{}, reader, testRuntimeDefaults()).Compile(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	var cfg claudeconfig.Config
	if err := json.Unmarshal(revision.ConfigJSON, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.SkillResources == nil || len(cfg.SkillResources.Skills) != 1 || cfg.SkillResources.Skills[0].Name != "review" ||
		len(cfg.SkillResources.Plugins) != 1 || !reflect.DeepEqual(cfg.SkillResources.Plugins[0].Skills, []string{"deploy"}) {
		t.Fatalf("compiled skills = %#v", cfg.SkillResources)
	}
	wantEgress := []string{"http://kagent-controller.kagent:8083", "https://api.anthropic.com:443", "https://git.example.com:443"}
	if !reflect.DeepEqual(revision.EgressDestinations, wantEgress) {
		t.Fatalf("egress = %v, want %v", revision.EgressDestinations, wantEgress)
	}

	input.Root.Template.Spec.Plugins[0].Skills = []string{"review"}
	if _, err := NewCompiler(krt.TestingDummyContext{}, reader, testRuntimeDefaults()).Compile(context.Background(), input); err == nil || !strings.Contains(err.Error(), "duplicate skill name") {
		t.Fatalf("duplicate skill Compile() error = %v", err)
	}
}

func TestCompileDirectWholeServerMCP(t *testing.T) {
	model := v1alpha3.ModelConfigSpec{
		Provider: v1alpha3.ModelProviderAnthropic, Model: "claude-sonnet-4-5", Headers: []v1alpha3.Header{{Name: "X-API-Key", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "api-key"}}}},
	}
	input, reader := testInput(t, model, map[string][]byte{
		"api-key": []byte("model-secret"), "mcp-token": []byte(credentialValue),
	})
	server := &v1alpha3.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "math-server", Namespace: "test", UID: "mcp-uid", Generation: 3},
		Spec: v1alpha3.RemoteMCPServerSpec{
			Protocol:       v1alpha3.RemoteMCPServerProtocolStreamableHttp,
			URL:            "https://mcp.example.com/mcp",
			SseReadTimeout: &metav1.Duration{Duration: 5 * time.Minute},
			Headers:        []v1alpha3.Header{{Name: "Authorization", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "mcp-token"}}}},
		},
		Status: v1alpha3.RemoteMCPServerStatus{ObservedGeneration: 3, DiscoveredTools: []*v1alpha3.MCPTool{
			{Name: "echo"}, {Name: "add_numbers"}, {Name: "get_time"},
		}},
	}
	input.Root.Template.Spec.Tools = []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{
		Server: corev1.TypedLocalObjectReference{Kind: "RemoteMCPServer", Name: server.Name},
		Tools:  []string{"get_time", "echo", "add_numbers"},
	}}}
	input.Root.MCPTools = []translator.ResolvedMCPTool{{Binding: *input.Root.Template.Spec.Tools[0].MCP.DeepCopy(), Server: server}}

	revision, err := NewCompiler(krt.TestingDummyContext{}, reader, testRuntimeDefaults()).Compile(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(revision.Warnings) != 0 {
		t.Fatalf("MCP compatibility warnings = %v", revision.Warnings)
	}
	var cfg claudeconfig.Config
	if err := json.Unmarshal(revision.ConfigJSON, &cfg); err != nil {
		t.Fatal(err)
	}
	compiled := cfg.MCPServers["math-server"]
	if compiled.Type != "http" || compiled.URL != server.Spec.URL || compiled.Headers["Authorization"] != translator.CredentialPlaceholder {
		t.Fatalf("compiled MCP = %#v", cfg.MCPServers)
	}
	if bytes.Contains(revision.ConfigJSON, []byte(credentialValue)) || bytes.Contains(revision.Provenance, []byte(credentialValue)) {
		t.Fatal("compiled MCP config or provenance contains credential material")
	}
	for _, variable := range revision.Environment {
		if variable.ValueFrom != nil {
			t.Fatal("unresolved credential environment")
		}
	}
	if len(revision.Credentials) != 2 {
		t.Fatalf("credentials = %v", revision.Credentials)
	}
	if !reflect.DeepEqual(revision.EgressDestinations, []string{"http://kagent-controller.kagent:8083", "https://api.anthropic.com:443", "https://mcp.example.com:443"}) {
		t.Fatalf("egress = %v", revision.EgressDestinations)
	}
	if !bytes.Contains(revision.Provenance, []byte(`"kind":"RemoteMCPServer"`)) {
		t.Fatalf("provenance omits RemoteMCPServer: %s", revision.Provenance)
	}
}

func TestCompileWholeServerMCPSelectionWarnings(t *testing.T) {
	model := v1alpha3.ModelConfigSpec{
		Provider: v1alpha3.ModelProviderAnthropic, Model: "claude-sonnet-4-5", Headers: []v1alpha3.Header{{Name: "X-API-Key", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "api-key"}}}},
	}
	input, reader := testInput(t, model, map[string][]byte{"api-key": []byte("secret")})
	server := &v1alpha3.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "tools", Namespace: "test", Generation: 1},
		Spec:       v1alpha3.RemoteMCPServerSpec{URL: "https://mcp.example.com/mcp"},
		Status: v1alpha3.RemoteMCPServerStatus{ObservedGeneration: 1, DiscoveredTools: []*v1alpha3.MCPTool{
			{Name: "one"}, {Name: "two"},
		}},
	}
	binding := v1alpha3.MCPToolBinding{Server: corev1.TypedLocalObjectReference{Kind: "RemoteMCPServer", Name: server.Name}}
	input.Root.MCPTools = []translator.ResolvedMCPTool{{Binding: binding, Server: server}}
	revision, err := NewCompiler(krt.TestingDummyContext{}, reader, testRuntimeDefaults()).Compile(context.Background(), input)
	if err != nil {
		t.Fatalf("omitted selection Compile() error = %v", err)
	}
	if len(revision.Warnings) != 0 {
		t.Fatalf("omitted selection warnings = %v", revision.Warnings)
	}

	input.Root.MCPTools[0].Binding.Tools = []string{"one"}
	revision, err = NewCompiler(krt.TestingDummyContext{}, reader, testRuntimeDefaults()).Compile(context.Background(), input)
	if err != nil {
		t.Fatalf("partial selection Compile() error = %v", err)
	}
	if len(revision.Warnings) != 1 || !strings.Contains(revision.Warnings[0], "exposing the whole server") {
		t.Fatalf("partial selection warnings = %v", revision.Warnings)
	}

	server.Status.ObservedGeneration = 0
	revision, err = NewCompiler(krt.TestingDummyContext{}, reader, testRuntimeDefaults()).Compile(context.Background(), input)
	if err != nil {
		t.Fatalf("stale discovery Compile() error = %v", err)
	}
	if len(revision.Warnings) != 1 || !strings.Contains(revision.Warnings[0], "no current discovered tool set") {
		t.Fatalf("stale discovery warnings = %v", revision.Warnings)
	}

	server.Status.ObservedGeneration = server.Generation
	input.Root.MCPTools[0].Binding.Tools = nil
	terminateOnClose := false
	server.Spec.TLS = &v1alpha3.TLSConfig{DisableVerify: true}
	server.Spec.Timeout = &metav1.Duration{Duration: time.Minute}
	server.Spec.TerminateOnClose = &terminateOnClose
	revision, err = NewCompiler(krt.TestingDummyContext{}, reader, testRuntimeDefaults()).Compile(context.Background(), input)
	if err != nil {
		t.Fatalf("unsupported MCP options Compile() error = %v", err)
	}
	if len(revision.Warnings) != 1 {
		t.Fatalf("unsupported MCP option warnings = %v", revision.Warnings)
	}
	for _, field := range []string{"custom TLS configuration", "timeout", "terminateOnClose"} {
		if !strings.Contains(revision.Warnings[0], field) {
			t.Errorf("unsupported MCP option warning %q omits %q", revision.Warnings[0], field)
		}
	}

	server.Spec.Protocol = v1alpha3.RemoteMCPServerProtocol("STDIO")
	if _, err := NewCompiler(krt.TestingDummyContext{}, reader, testRuntimeDefaults()).Compile(context.Background(), input); err == nil || !strings.Contains(err.Error(), "unsupported protocol") {
		t.Fatalf("unsupported MCP protocol Compile() error = %v", err)
	}

	server.Spec.Protocol = v1alpha3.RemoteMCPServerProtocolStreamableHttp
	server.Spec.TLS = nil
	server.Spec.Timeout = nil
	server.Spec.TerminateOnClose = nil
	input.Root.MCPTools[0].Binding.Tools = []string{"one"}
	input.Root.MCPTools[0].Binding.RequireApproval = true
	revision, err = NewCompiler(krt.TestingDummyContext{}, reader, testRuntimeDefaults()).Compile(context.Background(), input)
	if err != nil {
		t.Fatalf("approval-required MCP binding Compile() error = %v", err)
	}
	var compiled claudeconfig.Config
	if err := json.Unmarshal(revision.ConfigJSON, &compiled); err != nil {
		t.Fatal(err)
	}
	if !compiled.MCPServers["tools"].RequireApproval {
		t.Fatalf("approval-required MCP server = %#v", compiled.MCPServers["tools"])
	}
	if len(revision.Warnings) != 1 || !strings.Contains(revision.Warnings[0], "exposing the whole server") {
		t.Fatalf("approval-required partial selection warnings = %v", revision.Warnings)
	}
}

func TestCompileLocalSharedAgent(t *testing.T) {
	modelSpec := v1alpha3.ModelConfigSpec{
		Provider: v1alpha3.ModelProviderAnthropic, Model: "claude-root",

		Anthropic: &v1alpha3.AnthropicConfig{BaseURL: "https://gateway.example.com/anthropic"}, Headers: []v1alpha3.Header{{Name: "X-API-Key", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "api-key"}}}},
	}
	input, reader := testInput(t, modelSpec, map[string][]byte{"api-key": []byte("secret")})
	childModelSpec := modelSpec
	childModelSpec.Model = "claude-specialist"
	child := &translator.AgentInput{
		Template: &translator.TemplateConfiguration{
			Name: "specialist-template", Namespace: "test", Source: &metav1.ObjectMeta{Name: "specialist-template", Namespace: "test", UID: "child-template-uid"},
			Spec: v1alpha3.AgentTemplateSpec{
				ModelConfig: &corev1.LocalObjectReference{Name: "child-model"},
				Description: "template description", SystemPrompt: "specialize",
			},
		},
		ResolvedModelConfig: &translator.ResolvedModelConfig{Config: &v1alpha3.ModelConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "child-model", Namespace: "test", UID: "child-model-uid"},
			Spec:       childModelSpec,
		}},
		Instruction: "Return the specialist marker.",
	}
	input.Root.Template.Spec.Tools = []v1alpha3.ToolBinding{{SubAgent: &v1alpha3.SubAgentToolBinding{
		Name: "specialist", Description: "Handles specialist requests",
		TemplateRef: &corev1.LocalObjectReference{Name: child.Template.Name},
	}}}
	input.Root.Shared = []translator.AgentInputBinding{{
		Name: "specialist", Description: "Handles specialist requests", Agent: child,
	}}

	revision, err := NewCompiler(krt.TestingDummyContext{}, reader, testRuntimeDefaults()).Compile(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	var cfg claudeconfig.Config
	if err := json.Unmarshal(revision.ConfigJSON, &cfg); err != nil {
		t.Fatal(err)
	}
	want := claudeconfig.Agent{
		Description: "Handles specialist requests", Prompt: "Return the specialist marker.", Model: "claude-specialist",
	}
	if !reflect.DeepEqual(cfg.Agents, map[string]claudeconfig.Agent{"specialist": want}) {
		t.Fatalf("compiled agents = %#v, want specialist %#v", cfg.Agents, want)
	}
	for _, identity := range []string{`"name":"specialist-template"`, `"name":"child-model"`} {
		if !bytes.Contains(revision.Provenance, []byte(identity)) {
			t.Fatalf("provenance %s does not contain %s", revision.Provenance, identity)
		}
	}
}

func TestCompileRejectsUnsupportedLocalAgentConfiguration(t *testing.T) {
	modelSpec := v1alpha3.ModelConfigSpec{
		Provider: v1alpha3.ModelProviderAnthropic, Model: "claude-root", Headers: []v1alpha3.Header{{Name: "X-API-Key", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "api-key"}}}},
	}
	tests := []struct {
		name   string
		mutate func(*translator.AgentInputBinding)
		want   string
	}{
		{name: "provider configuration", mutate: func(binding *translator.AgentInputBinding) {
			binding.Agent.ResolvedModelConfig.Config.Spec.Headers[0].ValueFrom.SecretKeyRef.Name = "different-auth"
		}, want: "root agent's provider"},
		{name: "nested tools", mutate: func(binding *translator.AgentInputBinding) {
			binding.Agent.Template.Spec.Tools = []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{}}}
		}, want: "cannot contain MCP or nested agent tools"},
		{name: "skills", mutate: func(binding *translator.AgentInputBinding) {
			binding.Agent.Template.Spec.Skills = []v1alpha3.AgentTemplateSkill{{Name: "review"}}
		}, want: "cannot contain skills or plugins"},
		{name: "invalid binding name", mutate: func(binding *translator.AgentInputBinding) {
			binding.Name = "not valid"
		}, want: "invalid compiled Claude configuration"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, reader := testInput(t, modelSpec, map[string][]byte{"api-key": []byte("secret")})
			childSpec := *modelSpec.DeepCopy()
			childSpec.Model = "claude-child"
			binding := translator.AgentInputBinding{
				Name: "specialist", Description: "Handles specialist requests",
				Agent: &translator.AgentInput{
					Template: &translator.TemplateConfiguration{
						Name: "child", Namespace: "test", Source: &metav1.ObjectMeta{Name: "child", Namespace: "test"},
						Spec: v1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: "child-model"}},
					},
					ResolvedModelConfig: &translator.ResolvedModelConfig{Config: &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Name: "child-model", Namespace: "test"}, Spec: childSpec}},
					Instruction:         "specialize",
				},
			}
			tt.mutate(&binding)
			input.Root.Shared = []translator.AgentInputBinding{binding}
			_, err := NewCompiler(krt.TestingDummyContext{}, reader, testRuntimeDefaults()).Compile(context.Background(), input)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Compile() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func testInput(t *testing.T, modelSpec v1alpha3.ModelConfigSpec, secretData map[string][]byte) (*translator.HarnessInput, translator.Collections) {
	t.Helper()
	harness := &translator.HarnessConfiguration{Name: "claude", Namespace: "test", Source: metav1.ObjectMeta{Name: "claude", Namespace: "test", UID: "harness-uid"}, Spec: v1alpha3.HarnessSpec{
		Claude: &v1alpha3.ClaudeHarness{}, Workload: v1alpha3.HarnessWorkload{Image: new("example.com/claude@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")},
		Substrate: v1alpha3.RuntimeSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"}},
	}}
	template := &translator.TemplateConfiguration{Name: "assistant", Namespace: "test", Source: &metav1.ObjectMeta{Name: "assistant", Namespace: "test", UID: "template-uid"}, Spec: v1alpha3.AgentTemplateSpec{
		ModelConfig: &corev1.LocalObjectReference{Name: "model"}, Description: "assistant", SystemPrompt: "help carefully",
	}}
	model := &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Name: "model", Namespace: "test", UID: "model-uid"}, Spec: modelSpec}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "model-auth", Namespace: "test", UID: "secret-uid"}, Data: secretData}
	mock := krttest.NewMock(t, []any{secret})
	collections := translator.Collections{
		Secrets:    krttest.GetMockCollection[*corev1.Secret](mock),
		ConfigMaps: krttest.GetMockCollection[*corev1.ConfigMap](mock),
	}
	return &translator.HarnessInput{AgentName: "runnable-agent", Harness: harness, Root: &translator.AgentInput{Template: template, ResolvedModelConfig: &translator.ResolvedModelConfig{Config: model}, Instruction: "help carefully"}}, collections
}

func TestCompileRuntimeTelemetry(t *testing.T) {
	model := v1alpha3.ModelConfigSpec{
		Provider: v1alpha3.ModelProviderAnthropic, Model: "claude-sonnet-4-5", Headers: []v1alpha3.Header{{Name: "X-API-Key", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "api-key"}}}},
	}
	input, reader := testInput(t, model, map[string][]byte{"api-key": []byte("secret")})
	input.Harness.Name = "fast"

	settings := testRuntimeDefaults()
	revision, err := NewCompiler(krt.TestingDummyContext{}, reader, settings).Compile(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	config, err := claudeconfig.Parse(revision.ConfigJSON)
	if err != nil {
		t.Fatal(err)
	}
	// Changing the Harness name must not change the Agent runtime identity.
	want := tracing.RuntimeTelemetry{
		Runtime: tracing.RuntimeClaude, AgentName: "runnable-agent", AgentNamespace: "test",
		Provider: "anthropic", Model: "claude-sonnet-4-5",
	}
	if config.RuntimeTelemetry != want {
		t.Fatalf("runtime telemetry = %#v, want %#v", config.RuntimeTelemetry, want)
	}
	if config.RuntimeTelemetry.CaptureLimit() != 0 {
		t.Fatal("content capture is not disabled by default")
	}

	settings.Telemetry = &configv1.TelemetryConfig{CaptureContent: true, MaxCaptureBytes: new(int32(4096))}
	captured, err := NewCompiler(krt.TestingDummyContext{}, reader, settings).Compile(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	capturedConfig, err := claudeconfig.Parse(captured.ConfigJSON)
	if err != nil {
		t.Fatal(err)
	}
	if !capturedConfig.RuntimeTelemetry.CaptureContent || capturedConfig.RuntimeTelemetry.CaptureLimit() != 4096 {
		t.Fatalf("captured runtime telemetry = %#v", capturedConfig.RuntimeTelemetry)
	}
	// A telemetry change lives only in the compiled configuration, which the
	// revision digest covers. Provenance records Kubernetes inputs, none of
	// which changed.
	if !bytes.Equal(revision.Provenance, captured.Provenance) {
		t.Fatal("changing the capture policy changed revision provenance")
	}
	before, err := revision.Digest()
	if err != nil {
		t.Fatal(err)
	}
	after, err := captured.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("changing the capture policy did not change the revision digest")
	}
}

func TestCompiledTelemetryFitsTheActorEnvironmentBudget(t *testing.T) {
	settings := testRuntimeDefaults()
	settings.Telemetry = &configv1.TelemetryConfig{CaptureContent: true, CaptureRawApiBodies: true, MaxCaptureBytes: new(int32(4096)),
		Traces: &configv1.OTLPConfig{Endpoint: "http://traces:4317", Protocol: new("grpc")}, Metrics: &configv1.OTLPConfig{Endpoint: "http://collector:4317", Protocol: new("grpc")}, Logs: &configv1.OTLPConfig{Endpoint: "http://logs:4318/v1/logs", Protocol: new("http/protobuf")}}
	model := v1alpha3.ModelConfigSpec{
		Provider: v1alpha3.ModelProviderAnthropic, Model: "claude-sonnet-4-5", Headers: []v1alpha3.Header{{Name: "X-API-Key", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "api-key"}}}},
	}
	input, reader := testInput(t, model, map[string][]byte{"api-key": []byte("secret"), "mcp-token": []byte(credentialValue)})
	server := &v1alpha3.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "math-server", Namespace: "test", UID: "mcp-uid", Generation: 1},
		Spec: v1alpha3.RemoteMCPServerSpec{
			Protocol: v1alpha3.RemoteMCPServerProtocolStreamableHttp, URL: "https://mcp.example.com/mcp",
			Headers: []v1alpha3.Header{{Name: "Authorization", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "mcp-token"}}}},
		},
		Status: v1alpha3.RemoteMCPServerStatus{ObservedGeneration: 1, DiscoveredTools: []*v1alpha3.MCPTool{{Name: "echo"}}},
	}
	input.Root.Template.Spec.Tools = []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{
		Server: corev1.TypedLocalObjectReference{Kind: "RemoteMCPServer", Name: server.Name}, Tools: []string{"echo"},
	}}}
	input.Root.MCPTools = []translator.ResolvedMCPTool{{Binding: *input.Root.Template.Spec.Tools[0].MCP.DeepCopy(), Server: server}}

	revision, err := NewCompiler(krt.TestingDummyContext{}, reader, settings).Compile(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	// Placement and limits normally come from the central runtime compiler.
	revision.CPU, revision.Memory = "1", "1Gi"
	revisionID, err := revision.Digest()
	if err != nil {
		t.Fatal(err)
	}
	template, err := substrate.ActorTemplateForRevision(&revision.Revision, revisionID)
	if err != nil {
		t.Fatalf("worst-case Claude actor does not fit Substrate: %v", err)
	}
	t.Logf("worst-case Claude actor uses %d of 32 environment variables", len(template.GetContainers()[0].GetEnv()))
}

func testRuntimeDefaults() *configv1.RuntimeDefaultsConfig {
	return &configv1.RuntimeDefaultsConfig{ControlPlaneUrl: new("http://kagent-controller.kagent:8083"), GatewayUrl: new("http://kagent-controller.kagent:8083"), Logging: &configv1.LoggingConfig{Level: new("info")}}
}

func TestCompileMCPAuthentication(t *testing.T) {
	server := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "user"}, Spec: v1alpha3.RemoteMCPServerSpec{
		URL: "https://mcp.example.com/mcp", Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{TokenPassthrough: &v1alpha3.TokenPassthrough{}}}},
	}}
	_, err := compileMCP([]translator.ResolvedMCPTool{{Server: server}})
	require.ErrorContains(t, err, "does not support tokenPassthrough")
}
