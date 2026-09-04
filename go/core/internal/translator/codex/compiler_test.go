package codex

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
	codexconfig "github.com/kagent-dev/kagent/go/harness/codex/config"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const credentialValue = "credential-must-not-be-serialized"

func TestCompileSupportedProviders(t *testing.T) {
	responses := v1alpha3.OpenAIAPIFormatResponses
	tests := []struct {
		name        string
		model       v1alpha3.ModelConfigSpec
		secret      map[string][]byte
		provider    string
		environment map[string]string
		egress      []string
	}{
		{
			name: "OpenAI", model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-5.2-codex", APIKeySecret: "model-auth", APIKeySecretKey: "api-key", OpenAI: &v1alpha3.OpenAIConfig{APIFormat: &responses}},
			secret: map[string][]byte{"api-key": []byte(credentialValue)}, provider: "openai", environment: map[string]string{openAIAPIKeyEnv: credentialValue}, egress: []string{"api.openai.com"},
		},
		{
			name: "OpenAI gateway", model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt", APIKeySecret: "model-auth", APIKeySecretKey: "api-key", OpenAI: &v1alpha3.OpenAIConfig{APIFormat: &responses, BaseURL: "https://gateway.example.com/v1"}},
			secret: map[string][]byte{"api-key": []byte(credentialValue)}, provider: "openai", environment: map[string]string{openAIAPIKeyEnv: credentialValue}, egress: []string{"gateway.example.com"},
		},
		{
			name: "Bedrock API key", model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderBedrock, Model: "gpt-5.2", APIKeySecret: "model-auth", Bedrock: &v1alpha3.BedrockConfig{Region: "us-east-1", CacheTTL: "5m"}},
			secret: map[string][]byte{awsBedrockTokenEnv: []byte(credentialValue)}, provider: "amazon-bedrock", environment: map[string]string{awsRegionEnv: "us-east-1", awsBedrockTokenEnv: credentialValue}, egress: []string{"bedrock-runtime.us-east-1.amazonaws.com"},
		},
		{
			name: "Bedrock IAM", model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderBedrock, Model: "gpt-5.2", APIKeySecret: "model-auth", Bedrock: &v1alpha3.BedrockConfig{Region: "us-west-2"}},
			secret: map[string][]byte{awsAccessKeyEnv: []byte("access"), awsSecretKeyEnv: []byte(credentialValue), awsSessionTokenEnv: []byte("session")}, provider: "amazon-bedrock", environment: map[string]string{awsRegionEnv: "us-west-2", awsAccessKeyEnv: "access", awsSecretKeyEnv: credentialValue, awsSessionTokenEnv: "session"}, egress: []string{"bedrock-runtime.us-west-2.amazonaws.com"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input, reader := testInput(t, test.model, test.secret)
			revision, err := NewCompiler(krt.TestingDummyContext{}, reader).Compile(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			var cfg codexconfig.Config
			if err := json.Unmarshal(revision.ConfigJSON, &cfg); err != nil {
				t.Fatal(err)
			}
			if cfg.Provider.Name != test.provider || cfg.ExpectedCodexVersion != codexconfig.PinnedCodexVersion {
				t.Fatalf("config = %#v", cfg)
			}
			gotEnvironment := map[string]string{}
			for _, variable := range revision.Environment {
				if variable.ValueFrom != nil {
					t.Fatalf("unresolved environment = %#v", variable)
				}
				gotEnvironment[variable.Name] = variable.Value
			}
			for name, value := range test.environment {
				if gotEnvironment[name] != value {
					t.Errorf("environment[%s] = %q, want %q", name, gotEnvironment[name], value)
				}
			}
			if !reflect.DeepEqual(revision.EgressDestinations, test.egress) {
				t.Errorf("egress = %v, want %v", revision.EgressDestinations, test.egress)
			}
			if bytes.Contains(revision.ConfigJSON, []byte(credentialValue)) || bytes.Contains(revision.Provenance, []byte(credentialValue)) {
				t.Fatal("credential leaked into immutable revision")
			}
			again, err := NewCompiler(krt.TestingDummyContext{}, reader).Compile(context.Background(), input)
			if err != nil || !reflect.DeepEqual(revision, again) {
				t.Fatalf("compilation is not deterministic: %v", err)
			}
		})
	}
}

func TestCompileRejectsUnsupportedProviderConfiguration(t *testing.T) {
	responses, chat := v1alpha3.OpenAIAPIFormatResponses, v1alpha3.OpenAIAPIFormatChatCompletions
	tests := []v1alpha3.ModelConfigSpec{
		{Provider: v1alpha3.ModelProviderAnthropic, Model: "claude"},
		{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt", APIKeySecret: "model-auth", APIKeySecretKey: "api-key", OpenAI: &v1alpha3.OpenAIConfig{APIFormat: &chat}},
		{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt", APIKeySecret: "model-auth", APIKeySecretKey: "api-key", OpenAI: &v1alpha3.OpenAIConfig{APIFormat: &responses, Temperature: "1"}},
		{Provider: v1alpha3.ModelProviderBedrock, Model: "gpt", APIKeySecret: "model-auth", Bedrock: &v1alpha3.BedrockConfig{Region: "us-east-1", PromptCaching: true}},
	}
	for _, model := range tests {
		input, reader := testInput(t, model, map[string][]byte{"api-key": []byte("secret"), awsAccessKeyEnv: []byte("access"), awsSecretKeyEnv: []byte("secret")})
		_, err := NewCompiler(krt.TestingDummyContext{}, reader).Compile(context.Background(), input)
		var validation *v2translator.ValidationError
		if !errors.As(err, &validation) {
			t.Errorf("Compile(%s) error = %v, want validation", model.Provider, err)
		}
	}
}

func TestCompileMCPAndSharedAgent(t *testing.T) {
	responses := v1alpha3.OpenAIAPIFormatResponses
	model := v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-root", APIKeySecret: "model-auth", APIKeySecretKey: "api-key", OpenAI: &v1alpha3.OpenAIConfig{APIFormat: &responses}}
	input, reader := testInput(t, model, map[string][]byte{"api-key": []byte("secret"), "mcp-token": []byte(credentialValue)})
	server := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "tools", Namespace: "test", UID: "mcp"}, Spec: v1alpha3.RemoteMCPServerSpec{
		Protocol: v1alpha3.RemoteMCPServerProtocolStreamableHttp, URL: "https://mcp.example.com/mcp", HeadersFrom: []v1alpha3.ValueRef{{Name: "Authorization", ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.SecretValueSource, Name: "model-auth", Key: "mcp-token"}}},
	}}
	input.Root.MCPTools = []v2translator.ResolvedMCPTool{{Binding: v1alpha3.MCPToolBinding{Tools: []string{"read"}}, Server: server}}
	childModel := model
	childModel.Model = "gpt-child"
	input.Root.Shared = []v2translator.AgentInputBinding{{Name: "reviewer", Description: "Reviews", Agent: &v2translator.AgentInput{
		Template: &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: "test"}, Spec: v1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: "child-model"}}},
		ResolvedModelConfig: &v2translator.ResolvedModelConfig{
			Config: &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Name: "child-model", Namespace: "test"}, Spec: childModel},
		},
		Instruction: "Review carefully",
	}}}
	revision, err := NewCompiler(krt.TestingDummyContext{}, reader).Compile(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(revision.Warnings) != 0 {
		t.Fatalf("MCP compatibility warnings = %v", revision.Warnings)
	}
	var cfg codexconfig.Config
	if err := json.Unmarshal(revision.ConfigJSON, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Agents["reviewer"].Model != "gpt-child" || !reflect.DeepEqual(cfg.MCPServers["tools"].EnabledTools, []string{"read"}) {
		t.Fatalf("config = %#v", cfg)
	}
	if !strings.HasPrefix(cfg.MCPServers["tools"].Headers["Authorization"], "${"+mcpCredentialPrefix) {
		t.Fatalf("MCP headers = %#v", cfg.MCPServers["tools"].Headers)
	}
	if !reflect.DeepEqual(revision.EgressDestinations, []string{"api.openai.com", "mcp.example.com"}) {
		t.Fatalf("egress = %v", revision.EgressDestinations)
	}
}

func TestCompileMCPCompatibilityWarnings(t *testing.T) {
	responses := v1alpha3.OpenAIAPIFormatResponses
	model := v1alpha3.ModelConfigSpec{
		Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-root",
		APIKeySecret: "model-auth", APIKeySecretKey: "api-key",
		OpenAI: &v1alpha3.OpenAIConfig{APIFormat: &responses},
	}
	input, reader := testInput(t, model, map[string][]byte{"api-key": []byte("secret")})
	terminateOnClose := false
	server := &v1alpha3.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "tools", Namespace: "test", UID: "mcp"},
		Spec: v1alpha3.RemoteMCPServerSpec{
			Protocol:         v1alpha3.RemoteMCPServerProtocolStreamableHttp,
			URL:              "https://mcp.example.com/mcp",
			TLS:              &v1alpha3.TLSConfig{DisableVerify: true},
			Timeout:          &metav1.Duration{Duration: time.Minute},
			TerminateOnClose: &terminateOnClose,
		},
	}
	input.Root.MCPTools = []v2translator.ResolvedMCPTool{{Server: server}}

	compilation, err := NewCompiler(krt.TestingDummyContext{}, reader).Compile(context.Background(), input)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if len(compilation.Warnings) != 1 {
		t.Fatalf("MCP compatibility warnings = %v", compilation.Warnings)
	}
	for _, field := range []string{"custom TLS configuration", "timeout", "terminateOnClose"} {
		if !strings.Contains(compilation.Warnings[0], field) {
			t.Errorf("MCP compatibility warning %q omits %q", compilation.Warnings[0], field)
		}
	}
	var cfg codexconfig.Config
	if err := json.Unmarshal(compilation.ConfigJSON, &cfg); err != nil {
		t.Fatal(err)
	}
	if _, exists := cfg.MCPServers[server.Name]; !exists {
		t.Fatalf("config omits MCP server after compatibility warning: %#v", cfg.MCPServers)
	}

	server.Spec.Protocol = v1alpha3.RemoteMCPServerProtocolSse
	if _, err := NewCompiler(krt.TestingDummyContext{}, reader).Compile(context.Background(), input); err == nil || !strings.Contains(err.Error(), "requires Streamable HTTP") {
		t.Fatalf("unsupported MCP protocol Compile() error = %v", err)
	}
}

func testInput(t *testing.T, modelSpec v1alpha3.ModelConfigSpec, secretData map[string][]byte) (*v2translator.HarnessInput, v2translator.Collections) {
	t.Helper()
	harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Name: "codex", Namespace: "test", UID: "harness"}, Spec: v1alpha3.HarnessSpec{
		Codex: &v1alpha3.CodexHarness{}, Workload: v1alpha3.HarnessWorkload{Image: "example.com/codex@sha256:" + strings.Repeat("a", 64)},
		Substrate: v1alpha3.HarnessSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.HarnessSnapshotPolicy{Location: "snapshots"}},
	}}
	template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: "assistant", Namespace: "test", UID: "template"}, Spec: v1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: "model"}, Description: "assistant"}}
	model := &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Name: "model", Namespace: "test", UID: "model"}, Spec: modelSpec}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "model-auth", Namespace: "test", UID: "secret"}, Data: secretData}
	mock := krttest.NewMock(t, []any{secret})
	collections := v2translator.Collections{
		Secrets:    krttest.GetMockCollection[*corev1.Secret](mock),
		ConfigMaps: krttest.GetMockCollection[*corev1.ConfigMap](mock),
	}
	return &v2translator.HarnessInput{Harness: harness, Root: &v2translator.AgentInput{
		Template: template, ResolvedModelConfig: &v2translator.ResolvedModelConfig{Config: model}, Instruction: "help carefully",
	}}, collections
}
