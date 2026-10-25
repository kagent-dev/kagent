package codex

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
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	codexconfig "github.com/kagent-dev/kagent/go/harness/codex/config"
	"github.com/kagent-dev/kagent/go/pkg/telemetry"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

const credentialValue = "credential-must-not-be-serialized"

func TestCompileProviderCredentials(t *testing.T) {
	responses := v1alpha3.OpenAIAPIFormatResponses
	tests := []struct {
		name        string
		model       v1alpha3.ModelConfigSpec
		secret      map[string][]byte
		provider    string
		environment map[string]string
		egress      []string
		wantErr     string
	}{
		{
			name: "caller token passthrough is unsupported",
			model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Model: "model", OpenAI: &v1alpha3.OpenAIConfig{APIFormat: &responses},
				Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{TokenPassthrough: &v1alpha3.TokenPassthrough{}}}}},
			wantErr: "does not support tokenPassthrough",
		},
		{
			name: "OpenAI", model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-5.2-codex", OpenAI: &v1alpha3.OpenAIConfig{APIFormat: &responses}, Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "api-key"}}}}},
			secret: map[string][]byte{"api-key": []byte(credentialValue)}, provider: "openai", environment: map[string]string{openAIAPIKeyEnv: translator.CredentialPlaceholder}, egress: []string{"http://kagent-controller.kagent:8083", "https://api.openai.com:443"},
		},
		{
			name: "OpenAI gateway", model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt", OpenAI: &v1alpha3.OpenAIConfig{APIFormat: &responses, BaseURL: "https://gateway.example.com/v1"}, Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "api-key"}}}}},
			secret: map[string][]byte{"api-key": []byte(credentialValue)}, provider: "openai", environment: map[string]string{openAIAPIKeyEnv: translator.CredentialPlaceholder}, egress: []string{"http://kagent-controller.kagent:8083", "https://gateway.example.com:443"},
		},
		{
			name: "Bedrock API key", model: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderBedrock, Model: "gpt-5.2", Bedrock: &v1alpha3.BedrockConfig{Region: "us-east-1", CacheTTL: "5m"}, Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "AWS_BEARER_TOKEN_BEDROCK"}}}}},
			secret: map[string][]byte{awsBedrockTokenEnv: []byte(credentialValue)}, provider: "amazon-bedrock", environment: map[string]string{awsRegionEnv: "us-east-1", awsBedrockTokenEnv: translator.CredentialPlaceholder}, egress: []string{"http://kagent-controller.kagent:8083", "https://bedrock-runtime.us-east-1.amazonaws.com:443"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input, reader := testInput(t, test.model, test.secret)
			revision, err := NewCompiler(krt.TestingDummyContext{}, reader, testRuntimeDefaults()).Compile(context.Background(), input)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("expected unsupported credential error, got %v", err)
				}
				return
			}
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
			again, err := NewCompiler(krt.TestingDummyContext{}, reader, testRuntimeDefaults()).Compile(context.Background(), input)
			if err != nil || !reflect.DeepEqual(revision, again) {
				t.Fatalf("compilation is not deterministic: %v", err)
			}
		})
	}
}

func TestCompileAllowsRuntimeOTELEndpoint(t *testing.T) {
	responses := v1alpha3.OpenAIAPIFormatResponses
	model := v1alpha3.ModelConfigSpec{
		Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-5.2-codex",

		OpenAI: &v1alpha3.OpenAIConfig{APIFormat: &responses}, Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "api-key"}}}},
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
	responses := v1alpha3.OpenAIAPIFormatResponses
	model := v1alpha3.ModelConfigSpec{
		Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-5.2-codex",

		OpenAI: &v1alpha3.OpenAIConfig{APIFormat: &responses}, Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "api-key"}}}},
	}
	input, reader := testInput(t, model, map[string][]byte{"api-key": []byte("secret")})
	value := "x-tenant=team-a"
	input.Harness.Spec.Env = []v1alpha3.RuntimeEnvVar{{Name: "OTEL_EXPORTER_OTLP_HEADERS", Value: value}}

	result, err := NewCompiler(krt.TestingDummyContext{}, reader, testRuntimeDefaults()).Compile(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(result.Environment, func(v corev1.EnvVar) bool { return v.Name == input.Harness.Spec.Env[0].Name && v.Value == value }) {
		t.Fatal("runtime override missing")
	}
}

func TestCompileRejectsUnsupportedProviderConfiguration(t *testing.T) {
	responses, chat := v1alpha3.OpenAIAPIFormatResponses, v1alpha3.OpenAIAPIFormatChatCompletions
	tests := []v1alpha3.ModelConfigSpec{
		{Provider: v1alpha3.ModelProviderAnthropic, Model: "claude"},
		{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt", Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "api-key"}}}}, OpenAI: &v1alpha3.OpenAIConfig{APIFormat: &chat}},
		{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt", Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "api-key"}}}}, OpenAI: &v1alpha3.OpenAIConfig{APIFormat: &responses, Temperature: "1"}},
		{Provider: v1alpha3.ModelProviderBedrock, Model: "gpt", Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "AWS_BEARER_TOKEN_BEDROCK"}}}}, Bedrock: &v1alpha3.BedrockConfig{Region: "us-east-1", PromptCaching: true}},
	}
	for _, model := range tests {
		input, reader := testInput(t, model, map[string][]byte{"api-key": []byte("secret"), awsAccessKeyEnv: []byte("access"), awsSecretKeyEnv: []byte("secret")})
		_, err := NewCompiler(krt.TestingDummyContext{}, reader, testRuntimeDefaults()).Compile(context.Background(), input)
		var validation *translator.ValidationError
		if !errors.As(err, &validation) {
			t.Errorf("Compile(%s) error = %v, want validation", model.Provider, err)
		}
	}
}

func TestCompileMCPAndSharedAgent(t *testing.T) {
	responses := v1alpha3.OpenAIAPIFormatResponses
	model := v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-root", OpenAI: &v1alpha3.OpenAIConfig{APIFormat: &responses}, Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "api-key"}}}}}
	input, reader := testInput(t, model, map[string][]byte{"api-key": []byte("secret"), "mcp-token": []byte(credentialValue)})
	server := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "tools", Namespace: "test", UID: "mcp"}, Spec: v1alpha3.RemoteMCPServerSpec{
		Protocol: v1alpha3.RemoteMCPServerProtocolStreamableHttp, URL: "https://mcp.example.com/mcp", Headers: []v1alpha3.Header{{Name: "Authorization", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "mcp-token"}}}},
	}}
	input.Root.MCPTools = []translator.ResolvedMCPTool{{Binding: v1alpha3.MCPToolBinding{Tools: []string{"read"}, RequireApproval: true}, Server: server}}
	childModel := model
	childModel.Model = "gpt-child"
	input.Root.Shared = []translator.AgentInputBinding{{Name: "reviewer", Description: "Reviews", Agent: &translator.AgentInput{
		Template: &translator.TemplateConfiguration{Name: "child", Namespace: "test", Source: &metav1.ObjectMeta{Name: "child", Namespace: "test"}, Spec: v1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: "child-model"}}},
		ResolvedModelConfig: &translator.ResolvedModelConfig{
			Config: &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Name: "child-model", Namespace: "test"}, Spec: childModel},
		},
		Instruction: "Review carefully",
	}}}
	revision, err := NewCompiler(krt.TestingDummyContext{}, reader, testRuntimeDefaults()).Compile(context.Background(), input)
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
	if cfg.Agents["reviewer"].Model != "gpt-child" || !cfg.MCPServers["tools"].RequireApproval || !reflect.DeepEqual(cfg.MCPServers["tools"].EnabledTools, []string{"read"}) {
		t.Fatalf("config = %#v", cfg)
	}
	if cfg.MCPServers["tools"].Headers["Authorization"] != translator.CredentialPlaceholder {
		t.Fatalf("MCP headers = %#v", cfg.MCPServers["tools"].Headers)
	}
	if !reflect.DeepEqual(revision.EgressDestinations, []string{"http://kagent-controller.kagent:8083", "https://api.openai.com:443", "https://mcp.example.com:443"}) {
		t.Fatalf("egress = %v", revision.EgressDestinations)
	}
}

func TestCompileMCPCompatibilityWarnings(t *testing.T) {
	responses := v1alpha3.OpenAIAPIFormatResponses
	model := v1alpha3.ModelConfigSpec{
		Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-root",

		OpenAI: &v1alpha3.OpenAIConfig{APIFormat: &responses}, Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "api-key"}}}},
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
	input.Root.MCPTools = []translator.ResolvedMCPTool{{Binding: v1alpha3.MCPToolBinding{RequireApproval: true}, Server: server}}

	compilation, err := NewCompiler(krt.TestingDummyContext{}, reader, testRuntimeDefaults()).Compile(context.Background(), input)
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
	if configured, exists := cfg.MCPServers[server.Name]; !exists || !configured.RequireApproval || len(configured.EnabledTools) != 0 {
		t.Fatalf("config omits MCP server after compatibility warning: %#v", cfg.MCPServers)
	}

	server.Spec.Protocol = v1alpha3.RemoteMCPServerProtocolSse
	if _, err := NewCompiler(krt.TestingDummyContext{}, reader, testRuntimeDefaults()).Compile(context.Background(), input); err == nil || !strings.Contains(err.Error(), "requires Streamable HTTP") {
		t.Fatalf("unsupported MCP protocol Compile() error = %v", err)
	}
}

func testInput(t *testing.T, modelSpec v1alpha3.ModelConfigSpec, secretData map[string][]byte) (*translator.HarnessInput, translator.Collections) {
	t.Helper()
	harness := &translator.HarnessConfiguration{Name: "codex", Namespace: "test", Source: metav1.ObjectMeta{Name: "codex", Namespace: "test", UID: "harness"}, Spec: v1alpha3.HarnessSpec{
		Codex: &v1alpha3.CodexHarness{}, Workload: v1alpha3.HarnessWorkload{Image: new("example.com/codex@sha256:" + strings.Repeat("a", 64))},
		Substrate: v1alpha3.RuntimeSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"}},
	}}
	template := &translator.TemplateConfiguration{Name: "assistant", Namespace: "test", Source: &metav1.ObjectMeta{Name: "assistant", Namespace: "test", UID: "template"}, Spec: v1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: "model"}, Description: "assistant"}}
	model := &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Name: "model", Namespace: "test", UID: "model"}, Spec: modelSpec}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "model-auth", Namespace: "test", UID: "secret"}, Data: secretData}
	mock := krttest.NewMock(t, []any{secret})
	collections := translator.Collections{
		Secrets:    krttest.GetMockCollection[*corev1.Secret](mock),
		ConfigMaps: krttest.GetMockCollection[*corev1.ConfigMap](mock),
	}
	return &translator.HarnessInput{AgentName: "runnable-agent", Harness: harness, Root: &translator.AgentInput{
		Template: template, ResolvedModelConfig: &translator.ResolvedModelConfig{Config: model}, Instruction: "help carefully",
	}}, collections
}

func TestCompileRuntimeTelemetry(t *testing.T) {
	responses := v1alpha3.OpenAIAPIFormatResponses
	model := v1alpha3.ModelConfigSpec{
		Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-5.2-codex",

		OpenAI: &v1alpha3.OpenAIConfig{APIFormat: &responses}, Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}, Key: "api-key"}}}},
	}
	input, reader := testInput(t, model, map[string][]byte{"api-key": []byte("secret")})
	input.Harness.Name = "fast"

	settings := testRuntimeDefaults()
	settings.Telemetry = &configv1.TelemetryConfig{Traces: &configv1.OTLPConfig{Endpoint: "http://collector:4317", Protocol: new("grpc")}}
	revision, err := NewCompiler(krt.TestingDummyContext{}, reader, settings).Compile(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	config, err := codexconfig.Parse(revision.ConfigJSON)
	if err != nil {
		t.Fatal(err)
	}
	// Changing the Harness name must not change the Agent runtime identity.
	want := tracing.RuntimeTelemetry{
		Runtime: tracing.RuntimeCodex, AgentName: "runnable-agent", AgentNamespace: "test",
		Provider: "openai", Model: "gpt-5.2-codex",
		Exporters: &telemetry.Config{Traces: &telemetry.OTLPConfig{Endpoint: "http://collector:4317", Protocol: "grpc"}},
	}
	if !reflect.DeepEqual(config.RuntimeTelemetry, want) {
		t.Fatalf("runtime telemetry = %#v, want %#v", config.RuntimeTelemetry, want)
	}
	if config.RuntimeTelemetry.CaptureLimit() != 0 {
		t.Fatal("content capture is not disabled by default")
	}

	settings.Telemetry.CaptureContent = true
	captured, err := NewCompiler(krt.TestingDummyContext{}, reader, settings).Compile(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	capturedConfig, err := codexconfig.Parse(captured.ConfigJSON)
	if err != nil {
		t.Fatal(err)
	}
	if !capturedConfig.RuntimeTelemetry.CaptureContent {
		t.Fatalf("captured runtime telemetry = %#v", capturedConfig.RuntimeTelemetry)
	}
	// The native exporter settings stay separate from the Go wrapper contract.
	if capturedConfig.Telemetry == nil || !capturedConfig.Telemetry.CaptureContent {
		t.Fatalf("native telemetry = %#v", capturedConfig.Telemetry)
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
