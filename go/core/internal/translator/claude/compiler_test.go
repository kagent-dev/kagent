package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	claudeconfig "github.com/kagent-dev/kagent/go/harness/claude/config"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const credentialValue = "credential-must-not-be-serialized"

func TestCompileSupportedProviders(t *testing.T) {
	tests := []struct {
		name       string
		model      v1alpha3.ModelConfigSpec
		secretData map[string][]byte
		wantEnv    map[string]string
		wantEgress []string
	}{
		{
			name: "Anthropic",
			model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropic, Model: "claude-sonnet-4-5",
				APIKeySecret: "model-auth", APIKeySecretKey: "api-key"},
			secretData: map[string][]byte{"api-key": []byte(credentialValue)},
			wantEnv:    map[string]string{claudeconfig.AnthropicAPIKeyEnvName: credentialValue},
			wantEgress: []string{"api.anthropic.com"},
		},
		{
			name: "Anthropic gateway",
			model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropic, Model: "claude-sonnet-4-5",
				APIKeySecret: "model-auth", APIKeySecretKey: "api-key",
				Anthropic: &v1alpha3.AnthropicConfig{BaseURL: "http://host.docker.internal:8090/anthropic"}},
			secretData: map[string][]byte{"api-key": []byte(credentialValue)},
			wantEnv: map[string]string{claudeconfig.AnthropicAPIKeyEnvName: credentialValue,
				claudeconfig.AnthropicBaseURLEnvName: "http://host.docker.internal:8090/anthropic"},
			wantEgress: []string{"host.docker.internal"},
		},
		{
			name: "Bedrock IAM",
			model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderBedrock, Model: "us.anthropic.claude-sonnet-4-5-20250929-v1:0",
				APIKeySecret: "model-auth", Bedrock: &v1alpha3.BedrockConfig{Region: "us-east-1", CacheTTL: "5m"}},
			secretData: map[string][]byte{claudeconfig.AWSAccessKeyEnvName: []byte("access"), claudeconfig.AWSSecretKeyEnvName: []byte(credentialValue), claudeconfig.AWSSessionTokenEnvName: []byte("session")},
			wantEnv: map[string]string{claudeconfig.UseBedrockEnvName: "1", claudeconfig.AWSRegionEnvName: "us-east-1", claudeconfig.AWSAccessKeyEnvName: "access",
				claudeconfig.AWSSecretKeyEnvName: credentialValue, claudeconfig.AWSSessionTokenEnvName: "session"},
			wantEgress: []string{"bedrock-runtime.us-east-1.amazonaws.com"},
		},
		{
			name: "Bedrock API key",
			model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderBedrock, Model: "us.anthropic.claude-sonnet-4-5-20250929-v1:0",
				APIKeySecret: "model-auth", Bedrock: &v1alpha3.BedrockConfig{Region: "us-west-2"}},
			secretData: map[string][]byte{claudeconfig.AWSBedrockTokenEnvName: []byte(credentialValue)},
			wantEnv:    map[string]string{claudeconfig.UseBedrockEnvName: "1", claudeconfig.AWSRegionEnvName: "us-west-2", claudeconfig.AWSBedrockTokenEnvName: credentialValue},
			wantEgress: []string{"bedrock-runtime.us-west-2.amazonaws.com"},
		},
		{
			name: "Anthropic Vertex AI",
			model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropicVertexAI, Model: "claude-sonnet-4-5@20250929",
				APIKeySecret: "model-auth", APIKeySecretKey: "credentials.json",
				AnthropicVertexAI: &v1alpha3.AnthropicVertexAIConfig{BaseVertexAIConfig: v1alpha3.BaseVertexAIConfig{ProjectID: "project", Location: "us-east5"}}},
			secretData: map[string][]byte{"credentials.json": []byte(`{"type":"service_account","project_id":"project","token_uri":"https://oauth2.googleapis.com/token","private_key":"` + credentialValue + `"}`)},
			wantEnv: map[string]string{claudeconfig.UseVertexEnvName: "1", claudeconfig.VertexProjectEnvName: "project", claudeconfig.VertexRegionEnvName: "us-east5",
				claudeconfig.GoogleCredentialsJSONEnvName: `{"type":"service_account","project_id":"project","token_uri":"https://oauth2.googleapis.com/token","private_key":"` + credentialValue + `"}`},
			wantEgress: []string{"oauth2.googleapis.com", "us-east5-aiplatform.googleapis.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, reader := testInput(t, tt.model, tt.secretData)
			revision, err := NewCompiler(krt.TestingDummyContext{}, reader).Compile(context.Background(), input)
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
			if !bytes.Contains(revision.Provenance, []byte(`"kind":"Secret"`)) {
				t.Fatalf("provenance omits credential Secret: %s", revision.Provenance)
			}

			again, err := NewCompiler(krt.TestingDummyContext{}, reader).Compile(context.Background(), input)
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
		{name: "passthrough", model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropic, Model: "claude", APIKeyPassthrough: true}},
		{name: "headers", model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropic, Model: "claude", DefaultHeaders: map[string]string{"x": "y"}}},
		{name: "Anthropic options", model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropic, Model: "claude", Anthropic: &v1alpha3.AnthropicConfig{Temperature: "0.5"}}},
		{name: "Anthropic relative base URL", model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropic, Model: "claude", APIKeySecret: "model-auth", APIKeySecretKey: "api-key", Anthropic: &v1alpha3.AnthropicConfig{BaseURL: "/v1"}}},
		{name: "Anthropic base URL credentials", model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropic, Model: "claude", APIKeySecret: "model-auth", APIKeySecretKey: "api-key", Anthropic: &v1alpha3.AnthropicConfig{BaseURL: "https://user:password@example.com"}}},
		{name: "Bedrock options", model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderBedrock, Model: "claude", APIKeySecret: "model-auth", Bedrock: &v1alpha3.BedrockConfig{Region: "us-east-1", PromptCaching: true}}},
		{name: "Vertex options", model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderAnthropicVertexAI, Model: "claude", APIKeySecret: "model-auth", APIKeySecretKey: "credentials.json", AnthropicVertexAI: &v1alpha3.AnthropicVertexAIConfig{BaseVertexAIConfig: v1alpha3.BaseVertexAIConfig{ProjectID: "project", Location: "global", Temperature: "0.5"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, reader := testInput(t, tt.model, map[string][]byte{"api-key": []byte("secret"), claudeconfig.AWSAccessKeyEnvName: []byte("access"), claudeconfig.AWSSecretKeyEnvName: []byte("secret"), "credentials.json": []byte(`{"type":"service_account"}`)})
			_, err := NewCompiler(krt.TestingDummyContext{}, reader).Compile(context.Background(), input)
			var validation *v2translator.ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("Compile() error = %v, want validation error", err)
			}
		})
	}
}

func TestCompileRejectsProviderOwnedHarnessEnvironment(t *testing.T) {
	model := v1alpha3.ModelConfigSpec{
		Provider: v1alpha3.ModelProviderAnthropic, Model: "claude-sonnet-4-5",
		APIKeySecret: "model-auth", APIKeySecretKey: "api-key",
	}
	input, reader := testInput(t, model, map[string][]byte{"api-key": []byte("secret")})
	value := "http://mock.example.com"
	input.Harness.Spec.Env = []v1alpha3.HarnessEnvVar{{Name: claudeconfig.AnthropicBaseURLEnvName, Value: &value}}
	_, err := NewCompiler(krt.TestingDummyContext{}, reader).Compile(context.Background(), input)
	var validation *v2translator.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("Compile() error = %v, want validation error", err)
	}
}

func TestCompileRootSkillsAndPluginSelections(t *testing.T) {
	model := v1alpha3.ModelConfigSpec{
		Provider: v1alpha3.ModelProviderAnthropic, Model: "claude-sonnet-4-5",
		APIKeySecret: "model-auth", APIKeySecretKey: "api-key",
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

	revision, err := NewCompiler(krt.TestingDummyContext{}, reader).Compile(context.Background(), input)
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
	wantEgress := []string{"api.anthropic.com", "git.example.com", "registry.example.com"}
	if !reflect.DeepEqual(revision.EgressDestinations, wantEgress) {
		t.Fatalf("egress = %v, want %v", revision.EgressDestinations, wantEgress)
	}

	input.Root.Template.Spec.Plugins[0].Skills = []string{"review"}
	if _, err := NewCompiler(krt.TestingDummyContext{}, reader).Compile(context.Background(), input); err == nil || !strings.Contains(err.Error(), "duplicate skill name") {
		t.Fatalf("duplicate skill Compile() error = %v", err)
	}
}

func TestCompileDirectWholeServerMCP(t *testing.T) {
	model := v1alpha3.ModelConfigSpec{
		Provider: v1alpha3.ModelProviderAnthropic, Model: "claude-sonnet-4-5",
		APIKeySecret: "model-auth", APIKeySecretKey: "api-key",
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
			HeadersFrom: []v1alpha3.ValueRef{
				{Name: "X-Tenant", Value: "test"},
				{Name: "Authorization", ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.SecretValueSource, Name: "model-auth", Key: "mcp-token"}},
			},
		},
		Status: v1alpha3.RemoteMCPServerStatus{ObservedGeneration: 3, DiscoveredTools: []*v1alpha3.MCPTool{
			{Name: "echo"}, {Name: "add_numbers"}, {Name: "get_time"},
		}},
	}
	input.Root.Template.Spec.Tools = []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{
		Server: corev1.TypedLocalObjectReference{Kind: "RemoteMCPServer", Name: server.Name},
		Tools:  []string{"get_time", "echo", "add_numbers"},
	}}}
	input.Root.MCPTools = []v2translator.ResolvedMCPTool{{Binding: *input.Root.Template.Spec.Tools[0].MCP.DeepCopy(), Server: server}}

	revision, err := NewCompiler(krt.TestingDummyContext{}, reader).Compile(context.Background(), input)
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
	if compiled.Type != "http" || compiled.URL != server.Spec.URL || compiled.Headers["X-Tenant"] != "test" ||
		!strings.HasPrefix(compiled.Headers["Authorization"], "${"+claudeconfig.MCPCredentialEnvPrefix) {
		t.Fatalf("compiled MCP = %#v", cfg.MCPServers)
	}
	if bytes.Contains(revision.ConfigJSON, []byte(credentialValue)) || bytes.Contains(revision.Provenance, []byte(credentialValue)) {
		t.Fatal("compiled MCP config or provenance contains credential material")
	}
	foundSecret := false
	for _, variable := range revision.Environment {
		if strings.HasPrefix(variable.Name, claudeconfig.MCPCredentialEnvPrefix) && variable.Value == credentialValue {
			foundSecret = true
		}
	}
	if !foundSecret {
		t.Fatalf("MCP credential environment missing: %#v", revision.Environment)
	}
	if !reflect.DeepEqual(revision.EgressDestinations, []string{"api.anthropic.com", "mcp.example.com"}) {
		t.Fatalf("egress = %v", revision.EgressDestinations)
	}
	if !bytes.Contains(revision.Provenance, []byte(`"kind":"RemoteMCPServer"`)) {
		t.Fatalf("provenance omits RemoteMCPServer: %s", revision.Provenance)
	}
}

func TestCompileWholeServerMCPSelectionWarnings(t *testing.T) {
	model := v1alpha3.ModelConfigSpec{
		Provider: v1alpha3.ModelProviderAnthropic, Model: "claude-sonnet-4-5",
		APIKeySecret: "model-auth", APIKeySecretKey: "api-key",
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
	input.Root.MCPTools = []v2translator.ResolvedMCPTool{{Binding: binding, Server: server}}
	revision, err := NewCompiler(krt.TestingDummyContext{}, reader).Compile(context.Background(), input)
	if err != nil {
		t.Fatalf("omitted selection Compile() error = %v", err)
	}
	if len(revision.Warnings) != 0 {
		t.Fatalf("omitted selection warnings = %v", revision.Warnings)
	}

	input.Root.MCPTools[0].Binding.Tools = []string{"one"}
	revision, err = NewCompiler(krt.TestingDummyContext{}, reader).Compile(context.Background(), input)
	if err != nil {
		t.Fatalf("partial selection Compile() error = %v", err)
	}
	if len(revision.Warnings) != 1 || !strings.Contains(revision.Warnings[0], "exposing the whole server") {
		t.Fatalf("partial selection warnings = %v", revision.Warnings)
	}

	server.Status.ObservedGeneration = 0
	revision, err = NewCompiler(krt.TestingDummyContext{}, reader).Compile(context.Background(), input)
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
	revision, err = NewCompiler(krt.TestingDummyContext{}, reader).Compile(context.Background(), input)
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
	if _, err := NewCompiler(krt.TestingDummyContext{}, reader).Compile(context.Background(), input); err == nil || !strings.Contains(err.Error(), "unsupported protocol") {
		t.Fatalf("unsupported MCP protocol Compile() error = %v", err)
	}

	server.Spec.Protocol = v1alpha3.RemoteMCPServerProtocolStreamableHttp
	server.Spec.TLS = nil
	server.Spec.Timeout = nil
	server.Spec.TerminateOnClose = nil
	input.Root.MCPTools[0].Binding.Tools = []string{"one"}
	input.Root.MCPTools[0].Binding.RequireApproval = true
	revision, err = NewCompiler(krt.TestingDummyContext{}, reader).Compile(context.Background(), input)
	if err != nil {
		t.Fatalf("approval-required MCP binding Compile() error = %v", err)
	}
	var compiled claudeconfig.Config
	if err := json.Unmarshal(revision.Revision.ConfigJSON, &compiled); err != nil {
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
		APIKeySecret: "model-auth", APIKeySecretKey: "api-key",
		Anthropic: &v1alpha3.AnthropicConfig{BaseURL: "https://gateway.example.com/anthropic"},
	}
	input, reader := testInput(t, modelSpec, map[string][]byte{"api-key": []byte("secret")})
	childModelSpec := modelSpec
	childModelSpec.Model = "claude-specialist"
	child := &v2translator.AgentInput{
		Template: &v1alpha3.AgentTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "specialist-template", Namespace: "test", UID: "child-template-uid"},
			Spec: v1alpha3.AgentTemplateSpec{
				ModelConfig: &corev1.LocalObjectReference{Name: "child-model"},
				Description: "template description", SystemPrompt: "specialize",
			},
		},
		ResolvedModelConfig: &v2translator.ResolvedModelConfig{Config: &v1alpha3.ModelConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "child-model", Namespace: "test", UID: "child-model-uid"},
			Spec:       childModelSpec,
		}},
		Instruction: "Return the specialist marker.",
	}
	input.Root.Template.Spec.Tools = []v1alpha3.ToolBinding{{Agent: &v1alpha3.AgentToolBinding{
		Name: "specialist", Description: "Handles specialist requests",
		TemplateRef: corev1.LocalObjectReference{Name: child.Template.Name},
		Isolation:   v1alpha3.AgentToolIsolationShared,
	}}}
	input.Root.Shared = []v2translator.AgentInputBinding{{
		Name: "specialist", Description: "Handles specialist requests", Agent: child,
	}}

	revision, err := NewCompiler(krt.TestingDummyContext{}, reader).Compile(context.Background(), input)
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
		Provider: v1alpha3.ModelProviderAnthropic, Model: "claude-root",
		APIKeySecret: "model-auth", APIKeySecretKey: "api-key",
	}
	tests := []struct {
		name   string
		mutate func(*v2translator.AgentInputBinding)
		want   string
	}{
		{name: "provider configuration", mutate: func(binding *v2translator.AgentInputBinding) {
			binding.Agent.ResolvedModelConfig.Config.Spec.APIKeySecret = "different-auth"
		}, want: "root agent's provider"},
		{name: "nested tools", mutate: func(binding *v2translator.AgentInputBinding) {
			binding.Agent.Template.Spec.Tools = []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{}}}
		}, want: "cannot contain MCP or nested agent tools"},
		{name: "skills", mutate: func(binding *v2translator.AgentInputBinding) {
			binding.Agent.Template.Spec.Skills = []v1alpha3.AgentTemplateSkill{{Name: "review"}}
		}, want: "cannot contain skills or plugins"},
		{name: "invalid binding name", mutate: func(binding *v2translator.AgentInputBinding) {
			binding.Name = "not valid"
		}, want: "invalid compiled Claude configuration"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, reader := testInput(t, modelSpec, map[string][]byte{"api-key": []byte("secret")})
			childSpec := modelSpec
			childSpec.Model = "claude-child"
			binding := v2translator.AgentInputBinding{
				Name: "specialist", Description: "Handles specialist requests",
				Agent: &v2translator.AgentInput{
					Template: &v1alpha3.AgentTemplate{
						ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: "test"},
						Spec:       v1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: "child-model"}},
					},
					ResolvedModelConfig: &v2translator.ResolvedModelConfig{Config: &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Name: "child-model", Namespace: "test"}, Spec: childSpec}},
					Instruction:         "specialize",
				},
			}
			tt.mutate(&binding)
			input.Root.Shared = []v2translator.AgentInputBinding{binding}
			_, err := NewCompiler(krt.TestingDummyContext{}, reader).Compile(context.Background(), input)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Compile() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func testInput(t *testing.T, modelSpec v1alpha3.ModelConfigSpec, secretData map[string][]byte) (*v2translator.HarnessInput, v2translator.Collections) {
	t.Helper()
	harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Name: "claude", Namespace: "test", UID: "harness-uid"}, Spec: v1alpha3.HarnessSpec{
		Claude: &v1alpha3.ClaudeHarness{}, Workload: v1alpha3.HarnessWorkload{Image: "example.com/claude@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Substrate: v1alpha3.HarnessSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.HarnessSnapshotPolicy{Location: "snapshots"}},
	}}
	template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: "assistant", Namespace: "test", UID: "template-uid"}, Spec: v1alpha3.AgentTemplateSpec{
		ModelConfig: &corev1.LocalObjectReference{Name: "model"}, Description: "assistant", SystemPrompt: "help carefully",
	}}
	model := &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Name: "model", Namespace: "test", UID: "model-uid"}, Spec: modelSpec}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "model-auth", Namespace: "test", UID: "secret-uid"}, Data: secretData}
	mock := krttest.NewMock(t, []any{secret})
	collections := v2translator.Collections{
		Secrets:    krttest.GetMockCollection[*corev1.Secret](mock),
		ConfigMaps: krttest.GetMockCollection[*corev1.ConfigMap](mock),
	}
	return &v2translator.HarnessInput{Harness: harness, Root: &v2translator.AgentInput{Template: template, ResolvedModelConfig: &v2translator.ResolvedModelConfig{Config: model}, Instruction: "help carefully"}}, collections
}
