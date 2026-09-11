package translator_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	byotranslator "github.com/kagent-dev/kagent/go/core/internal/translator/byo"
	kagenttranslator "github.com/kagent-dev/kagent/go/core/internal/translator/kagent"
	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func modelConfig() *v1alpha3.ModelConfig {
	return &v1alpha3.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "default-model", Namespace: "test"},
		Spec:       v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-4o"},
	}
}

func TestCompileAgentTemplatePinsAgentPluginSources(t *testing.T) {
	embeddingModel := modelConfig()
	embeddingModel.Name = "embedding-model"
	embeddingModel.Spec.Model = "text-embedding-3-small"
	embeddingModel.Spec.TLS = &v1alpha3.TLSConfig{DisableVerify: true}
	harness := &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{Name: "kagent", Namespace: "test"},
		Spec: v1alpha3.HarnessSpec{
			Kagent: &v1alpha3.KagentHarness{Memory: &v1alpha3.KagentHarnessMemory{
				ModelConfigRef: corev1.LocalObjectReference{Name: embeddingModel.Name}, TTLDays: 7,
			}},
			AllowedAgentTemplates: &v1alpha3.HarnessAgentTemplateAdmission{Selector: metav1.LabelSelector{}},
			Workload:              v1alpha3.HarnessWorkload{Image: "example.com/kagent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			Substrate: v1alpha3.HarnessSubstratePolicy{
				WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.HarnessSnapshotPolicy{Location: "snapshots"},
			},
		},
	}
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "helper", Namespace: "test"},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig: &corev1.LocalObjectReference{Name: "default-model"},
			Skills: []v1alpha3.AgentTemplateSkill{
				{Name: "review", Source: v1alpha3.ArtifactSource{
					OCI: "ghcr.io/acme/review@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				}},
				{Name: "summary", Source: v1alpha3.ArtifactSource{
					OCI: "acme/summary@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
				}},
			},
			Plugins: []v1alpha3.PluginBundle{
				{
					Source: v1alpha3.ArtifactSource{Git: &v1alpha3.GitArtifact{
						URL: "https://github.com/acme/plugin", Commit: "cccccccccccccccccccccccccccccccccccccccc",
					}},
					Skills: []string{"deploy"},
				},
				{Source: v1alpha3.ArtifactSource{Bucket: &v1alpha3.BucketArtifact{S3: v1alpha3.S3Object{
					Endpoint: "https://objects.example.com", Bucket: "plugins", Key: "plugin.zip", VersionID: "version-1",
				}}}},
			},
		},
	}
	spec, err := compiler(t, modelConfig(), embeddingModel).CompileAgentTemplate(context.Background(), harness, template)
	if err != nil {
		t.Fatal(err)
	}
	var config adk.AgentConfig
	if err := json.Unmarshal(spec.ConfigJSON, &config); err != nil {
		t.Fatal(err)
	}
	// The driver is part of the assertion, not incidental. The Python runtime opens
	// this URL with an asyncio engine and refuses a bare `sqlite:` one, so dropping
	// the driver leaves an actor that never serves /readyz — which surfaces as a
	// harness stuck in ResumeGoldenActor rather than as anything naming this line.
	if config.SessionDBURL != "sqlite+aiosqlite:////data/sessions.db" {
		t.Fatalf("session DB URL = %q", config.SessionDBURL)
	}
	if config.Memory == nil || config.Memory.TTLDays != 7 || config.Memory.Embedding == nil || config.Memory.Embedding.TLSInsecureSkipVerify == nil || !*config.Memory.Embedding.TLSInsecureSkipVerify {
		t.Fatalf("compiled memory config = %#v", config.Memory)
	}
	plugins := config.AgentPlugins
	if plugins == nil || len(plugins.Skills) != 2 || len(plugins.Plugins) != 2 || plugins.Plugins[0].Source.Git.Commit != "cccccccccccccccccccccccccccccccccccccccc" {
		t.Fatalf("compiled Agent Plugins config = %#v", config)
	}
	for _, host := range []string{"ghcr.io", "registry-1.docker.io", "github.com", "objects.example.com"} {
		if !slices.Contains(spec.EgressDestinations, host) {
			t.Fatalf("egress destinations %v do not contain %q", spec.EgressDestinations, host)
		}
	}
}

func remoteMCPServer(name, url string) *v1alpha3.RemoteMCPServer {
	return &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test"}, Spec: v1alpha3.RemoteMCPServerSpec{
		URL: url, Protocol: v1alpha3.RemoteMCPServerProtocolStreamableHttp,
	}}
}

func compiler(t *testing.T, objects ...any) *v2translator.Compiler {
	t.Helper()
	collections := mockCollections(t, objects...)
	ctx := krt.TestingDummyContext{}
	return v2translator.NewCompiler(ctx, collections, map[v2translator.HarnessType]v2translator.HarnessCompiler{
		v2translator.HarnessTypeKagent: kagenttranslator.NewCompiler(ctx, collections),
		v2translator.HarnessTypeBYO:    byotranslator.NewCompiler(ctx, collections),
	})
}

func mockCollections(t *testing.T, objects ...any) v2translator.Collections {
	t.Helper()
	mock := krttest.NewMock(t, objects)
	collections := v2translator.Collections{
		AgentTemplates:   krttest.GetMockCollection[*v1alpha3.AgentTemplate](mock),
		RemoteMCPServers: krttest.GetMockCollection[*v1alpha3.RemoteMCPServer](mock),
		ConfigMaps:       krttest.GetMockCollection[*corev1.ConfigMap](mock),
		Secrets:          krttest.GetMockCollection[*corev1.Secret](mock),
	}
	models := krttest.GetMockCollection[*v1alpha3.ModelConfig](mock)
	resolved := make([]any, 0, len(models.List()))
	for _, model := range models.List() {
		value, err := v2translator.ResolveModelConfig(krt.TestingDummyContext{}, collections, model)
		require.NoError(t, err)
		resolved = append(resolved, *value)
	}
	resolvedMock := krttest.NewMock(t, resolved)
	collections.ResolvedModelConfigs = krttest.GetMockCollection[v2translator.ResolvedModelConfig](resolvedMock)
	return collections
}

func TestResolveModelConfigFoundryEndpoint(t *testing.T) {
	ref := &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "account"}, Key: "endpoint"}
	const endpoint = "https://example.services.ai.azure.com"
	for _, tt := range []struct {
		name      string
		foundry   *v1alpha3.FoundryConfig
		data      map[string]string
		endpoint  string
		failure   string
		reference bool
	}{
		{name: "inline", foundry: &v1alpha3.FoundryConfig{Endpoint: endpoint}, endpoint: endpoint},
		{name: "inline takes precedence", foundry: &v1alpha3.FoundryConfig{Endpoint: endpoint, EndpointFrom: ref}, endpoint: endpoint},
		{name: "ConfigMap", foundry: &v1alpha3.FoundryConfig{EndpointFrom: ref}, data: map[string]string{"endpoint": endpoint}, endpoint: endpoint, reference: true},
		{name: "missing ConfigMap", foundry: &v1alpha3.FoundryConfig{EndpointFrom: ref}, failure: "EndpointConfigMapNotFound", reference: true},
		{name: "missing key", foundry: &v1alpha3.FoundryConfig{EndpointFrom: ref}, data: map[string]string{}, failure: "EndpointConfigMapKeyNotFound", reference: true},
		{name: "empty endpoint", foundry: &v1alpha3.FoundryConfig{EndpointFrom: ref}, data: map[string]string{"endpoint": ""}, failure: "EndpointConfigMapKeyEmpty", reference: true},
		{name: "missing endpoint", foundry: &v1alpha3.FoundryConfig{}, failure: "InvalidProviderConfig"},
		{name: "missing provider config", failure: "InvalidProviderConfig"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			model := &v1alpha3.ModelConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "foundry", Namespace: "test"},
				Spec:       v1alpha3.ModelConfigSpec{Model: "gpt-4o", Provider: v1alpha3.ModelProviderFoundry, Foundry: tt.foundry},
			}
			original := model.DeepCopy()
			objects := []any{model}
			if tt.data != nil {
				objects = append(objects, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: ref.Name, Namespace: "test"}, Data: tt.data})
			}
			resolved := mockCollections(t, objects...).ResolvedModelConfigs.List()[0]
			require.Equal(t, original, model, "resolution must not mutate its input")
			require.Equal(t, original, resolved.Config, "retain the source configuration separately")
			require.Equal(t, tt.endpoint, resolved.FoundryEndpoint)
			if tt.failure == "" {
				require.True(t, resolved.Usable())
			} else {
				require.False(t, resolved.Usable())
				require.Equal(t, tt.failure, resolved.Failure().Reason)
			}
			if tt.reference {
				require.Equal(t, []v2translator.ModelConfigReference{{
					NamespacedName: types.NamespacedName{Namespace: "test", Name: ref.Name}, Kind: "ConfigMap", Key: ref.Key,
				}}, resolved.References)
			} else {
				require.Empty(t, resolved.References)
			}
		})
	}
}

type testHarnessCompiler struct{ input *v2translator.HarnessInput }

func (c *testHarnessCompiler) Compile(_ context.Context, input *v2translator.HarnessInput) (*v2translator.CompileResult, error) {
	c.input = input
	return &v2translator.CompileResult{Revision: v2translator.Revision{AgentTemplateName: input.Root.Template.Name}}, nil
}

func TestCompilerAcceptsExternalHarnessCompiler(t *testing.T) {
	collections := mockCollections(t, modelConfig())
	adapter := &testHarnessCompiler{}
	harness := &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{Name: "codex", Namespace: "test"},
		Spec:       v1alpha3.HarnessSpec{Codex: &v1alpha3.CodexHarness{}, AllowedAgentTemplates: &v1alpha3.HarnessAgentTemplateAdmission{Selector: metav1.LabelSelector{}}},
	}
	template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: "assistant", Namespace: "test"}, Spec: v1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: "default-model"}}}

	revision, err := v2translator.NewCompiler(krt.TestingDummyContext{}, collections, map[v2translator.HarnessType]v2translator.HarnessCompiler{
		v2translator.HarnessTypeCodex: adapter,
	}).CompileAgentTemplate(context.Background(), harness, template)
	require.NoError(t, err)
	require.Equal(t, "assistant", revision.AgentTemplateName)
	require.Equal(t, template.Name, adapter.input.Root.Template.Name)
	require.Equal(t, modelConfig().Spec, adapter.input.Root.ResolvedModelConfig.Config.Spec)
}

func TestCompilerRejectsUnusableModelConfigBeforeHarnessCompiler(t *testing.T) {
	model := modelConfig()
	model.Spec.APIKeySecret = "missing"
	model.Spec.APIKeySecretKey = "key"
	collections := mockCollections(t, model)
	adapter := &testHarnessCompiler{}
	harness := &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{Name: "codex", Namespace: "test"},
		Spec:       v1alpha3.HarnessSpec{Codex: &v1alpha3.CodexHarness{}, AllowedAgentTemplates: &v1alpha3.HarnessAgentTemplateAdmission{Selector: metav1.LabelSelector{}}},
	}
	template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: "assistant", Namespace: "test"}, Spec: v1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: model.Name}}}

	_, err := v2translator.NewCompiler(krt.TestingDummyContext{}, collections, map[v2translator.HarnessType]v2translator.HarnessCompiler{
		v2translator.HarnessTypeCodex: adapter,
	}).CompileAgentTemplate(context.Background(), harness, template)
	require.ErrorContains(t, err, `resolve ModelConfig "default-model": secret missing not found`)
	require.Nil(t, adapter.input)
}

func TestCompilerPermitsBYOWithoutModelConfig(t *testing.T) {
	adapter := &testHarnessCompiler{}
	harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Name: "byo", Namespace: "test"}, Spec: v1alpha3.HarnessSpec{
		BYO: &v1alpha3.BYOHarness{}, AllowedAgentTemplates: &v1alpha3.HarnessAgentTemplateAdmission{Selector: metav1.LabelSelector{}},
	}}
	template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: "assistant", Namespace: "test"}}

	_, err := v2translator.NewCompiler(krt.TestingDummyContext{}, mockCollections(t), map[v2translator.HarnessType]v2translator.HarnessCompiler{
		v2translator.HarnessTypeBYO: adapter,
	}).CompileAgentTemplate(context.Background(), harness, template)
	require.NoError(t, err)
	require.Nil(t, adapter.input.Root.ResolvedModelConfig)
}

func TestCompileAgentTemplateInjectsCredentialsAtGateway(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "mcp-auth", Namespace: "test"},
		Data:       map[string][]byte{"token": []byte("Bearer top-secret")},
	}
	secondSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "second-mcp-auth", Namespace: "test"},
		Data:       map[string][]byte{"token": []byte("Bearer another-secret")},
	}
	server := remoteMCPServer("remote", "https://mcp.example.com/mcp")
	server.Spec.HeadersFrom = []v1alpha3.ValueRef{{
		Name: "Authorization",
		ValueFrom: &v1alpha3.ValueSource{
			Type: v1alpha3.SecretValueSource, Name: secret.Name, Key: "token",
		},
	}}
	secondServer := remoteMCPServer("second-remote", "https://second-mcp.example.com/mcp")
	secondServer.Spec.HeadersFrom = []v1alpha3.ValueRef{{
		Name: "Authorization",
		ValueFrom: &v1alpha3.ValueSource{
			Type: v1alpha3.SecretValueSource, Name: secondSecret.Name, Key: "token",
		},
	}}
	harness := &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{Name: "kagent", Namespace: "test"},
		Spec: v1alpha3.HarnessSpec{
			Kagent:                &v1alpha3.KagentHarness{},
			AllowedAgentTemplates: &v1alpha3.HarnessAgentTemplateAdmission{Selector: metav1.LabelSelector{}},
			Workload:              v1alpha3.HarnessWorkload{Image: "example.com/kagent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			Substrate: v1alpha3.HarnessSubstratePolicy{
				WorkerPoolRef:  corev1.LocalObjectReference{Name: "default"},
				SnapshotPolicy: v1alpha3.HarnessSnapshotPolicy{Location: "snapshots"},
			},
		},
	}
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "helper", Namespace: "test"},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  &corev1.LocalObjectReference{Name: "default-model"},
			SystemPrompt: "help",
			Tools: []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{
				Server: corev1.TypedLocalObjectReference{Kind: "RemoteMCPServer", Name: server.Name},
				Tools:  []string{"lookup"},
			}}, {MCP: &v1alpha3.MCPToolBinding{
				Server: corev1.TypedLocalObjectReference{Kind: "RemoteMCPServer", Name: secondServer.Name},
				Tools:  []string{"search"},
			}}},
		},
	}
	compiler := compiler(t, modelConfig(), server, secondServer, secret, secondSecret)
	spec, err := compiler.CompileAgentTemplate(context.Background(), harness, template)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(spec.ConfigJSON, secret.Data["token"]) || bytes.Contains(spec.Provenance, secret.Data["token"]) {
		t.Fatal("runtime revision contains credential value")
	}
	if count := bytes.Count(spec.Provenance, []byte(`"kind":"Secret"`)); count != 0 {
		t.Fatalf("provenance contains %d Secret entries, want 0: %s", count, spec.Provenance)
	}
	if !bytes.Contains(spec.ConfigJSON, []byte("__KAGENT_ENV[KAGENT_CREDENTIAL_")) {
		t.Fatalf("config does not contain credential placeholder: %s", spec.ConfigJSON)
	}
	foundSecretValues := map[string]bool{}
	for _, variable := range spec.Environment {
		if variable.ValueFrom != nil {
			t.Fatalf("runtime revision environment contains unresolved valueFrom: %+v", variable)
		}
		foundSecretValues[variable.Value] = true
	}
	if foundSecretValues[string(secret.Data["token"])] || foundSecretValues[string(secondSecret.Data["token"])] {
		t.Fatal("credential leaked into runtime environment")
	}
	require.True(t, foundSecretValues[v2translator.CredentialPlaceholder])
	require.Len(t, spec.Credentials, 2)
	firstDigest, err := spec.Digest()
	require.NoError(t, err)
	rotated := secret.DeepCopy()
	rotated.UID = "replacement-secret"
	rotated.Data["token"] = []byte("rotated-token")
	rotatedCompiler := v2translator.NewCompiler(krt.TestingDummyContext{}, mockCollections(t, modelConfig(), server, secondServer, rotated, secondSecret), map[v2translator.HarnessType]v2translator.HarnessCompiler{
		v2translator.HarnessTypeKagent: kagenttranslator.NewCompiler(krt.TestingDummyContext{}, mockCollections(t, modelConfig(), server, secondServer, rotated, secondSecret)),
	})
	next, err := rotatedCompiler.CompileAgentTemplate(t.Context(), harness, template)
	require.NoError(t, err)
	nextDigest, err := next.Digest()
	require.NoError(t, err)
	require.Equal(t, firstDigest, nextDigest, "gateway credential rotation must not change runtime revision")
	if len(spec.EgressDestinations) != 3 || spec.EgressDestinations[0] != "api.openai.com" || spec.EgressDestinations[1] != "mcp.example.com" || spec.EgressDestinations[2] != "second-mcp.example.com" {
		t.Fatalf("egress destinations = %v", spec.EgressDestinations)
	}
}

func TestCompileAgentTemplateForwardsOtelEnvironment(t *testing.T) {
	t.Setenv("OTEL_TRACING_ENABLED", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4317")
	t.Setenv("OTEL_LOGGING_ENABLED", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "http://logs:4318/v1/logs")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL", "http/protobuf")
	harness := &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{Name: "kagent", Namespace: "test"},
		Spec: v1alpha3.HarnessSpec{
			Kagent:                &v1alpha3.KagentHarness{},
			AllowedAgentTemplates: &v1alpha3.HarnessAgentTemplateAdmission{Selector: metav1.LabelSelector{}},
			Workload:              v1alpha3.HarnessWorkload{Image: "example.com/kagent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			Substrate: v1alpha3.HarnessSubstratePolicy{
				WorkerPoolRef:  corev1.LocalObjectReference{Name: "default"},
				SnapshotPolicy: v1alpha3.HarnessSnapshotPolicy{Location: "snapshots"},
			},
		},
	}
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "helper", Namespace: "test"},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  &corev1.LocalObjectReference{Name: "default-model"},
			SystemPrompt: "help",
		},
	}
	spec, err := compiler(t, modelConfig()).CompileAgentTemplate(context.Background(), harness, template)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	for _, variable := range spec.Environment {
		found[variable.Name] = variable.Value
	}
	for name, value := range map[string]string{
		"OTEL_TRACING_ENABLED": "true", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "http://collector:4317",
		"OTEL_LOGGING_ENABLED": "true", "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT": "http://logs:4318/v1/logs",
		"OTEL_EXPORTER_OTLP_LOGS_PROTOCOL": "http/protobuf",
	} {
		if found[name] != value {
			t.Errorf("environment[%s] = %q, want %q", name, found[name], value)
		}
	}
	for _, hostname := range []string{"collector", "logs"} {
		if !slices.Contains(spec.EgressDestinations, hostname) {
			t.Errorf("%s missing from egress destinations: %v", hostname, spec.EgressDestinations)
		}
	}
}

func TestCompileAgentTemplateSharedAgent(t *testing.T) {
	selector := &v1alpha3.HarnessAgentTemplateAdmission{Selector: metav1.LabelSelector{MatchLabels: map[string]string{"runtime": "kagent"}}}
	harness := &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{Name: "kagent", Namespace: "test"},
		Spec: v1alpha3.HarnessSpec{
			Kagent: &v1alpha3.KagentHarness{}, AllowedAgentTemplates: selector,
			Workload:  v1alpha3.HarnessWorkload{Image: "example.com/kagent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			Substrate: v1alpha3.HarnessSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.HarnessSnapshotPolicy{Location: "snapshots"}},
		},
	}
	child := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "researcher", Namespace: "test", Labels: map[string]string{"runtime": "kagent"}},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig: &corev1.LocalObjectReference{Name: "default-model"}, Description: "template description", SystemPrompt: "research carefully",
			Tools: []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{
				Server: corev1.TypedLocalObjectReference{Kind: "RemoteMCPServer", Name: "search"}, Tools: []string{"lookup"},
			}}},
			Skills: []v1alpha3.AgentTemplateSkill{{Name: "review", Source: v1alpha3.ArtifactSource{
				OCI: "ghcr.io/acme/review@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			}}},
		},
	}
	root := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "coordinator", Namespace: "test", Labels: map[string]string{"runtime": "kagent"}},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig: &corev1.LocalObjectReference{Name: "default-model"}, SystemPrompt: "coordinate",
			Tools: []v1alpha3.ToolBinding{{Agent: &v1alpha3.AgentToolBinding{
				Name: "web_researcher", Description: "research the web", TemplateRef: corev1.LocalObjectReference{Name: child.Name},
			}}},
		},
	}
	revision, err := compiler(t, modelConfig(), child, remoteMCPServer("search", "https://search.example.com/mcp")).CompileAgentTemplate(context.Background(), harness, root)
	require.NoError(t, err)
	var config adk.AgentConfig
	require.NoError(t, json.Unmarshal(revision.ConfigJSON, &config))
	require.Len(t, config.SubAgents, 1)
	require.Equal(t, "web_researcher", config.SubAgents[0].Name)
	require.Equal(t, "research the web", config.SubAgents[0].Description)
	require.Equal(t, "research carefully", config.SubAgents[0].Instruction)
	require.Equal(t, []string{"lookup"}, config.SubAgents[0].HttpTools[0].Tools)
	require.Equal(t, "review", config.SubAgents[0].AgentPlugins.Skills[0].Name)
	require.Contains(t, revision.EgressDestinations, "search.example.com")
	require.Contains(t, revision.EgressDestinations, "ghcr.io")
	require.Contains(t, string(revision.Provenance), `"name":"researcher"`)
}

func TestCompileAgentTemplateRejectsInvalidSharedTrees(t *testing.T) {
	selector := &v1alpha3.HarnessAgentTemplateAdmission{Selector: metav1.LabelSelector{MatchLabels: map[string]string{"runtime": "kagent"}}}
	harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Name: "kagent", Namespace: "test"}, Spec: v1alpha3.HarnessSpec{
		Kagent: &v1alpha3.KagentHarness{}, AllowedAgentTemplates: selector,
	}}
	binding := func(name, target string) v1alpha3.ToolBinding {
		return v1alpha3.ToolBinding{Agent: &v1alpha3.AgentToolBinding{Name: name, Description: name, TemplateRef: corev1.LocalObjectReference{Name: target}}}
	}
	template := func(name string, tools ...v1alpha3.ToolBinding) *v1alpha3.AgentTemplate {
		return &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test", Labels: map[string]string{"runtime": "kagent"}}, Spec: v1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: "default-model"}, Tools: tools}}
	}

	t.Run("shared DAG", func(t *testing.T) {
		child := template("child")
		root := template("root", binding("first", child.Name), binding("second", child.Name))
		_, err := compiler(t, child).CompileAgentTemplate(context.Background(), harness, root)
		require.ErrorContains(t, err, "referenced more than once")
	})
	t.Run("cycle", func(t *testing.T) {
		root := template("root", binding("child", "child"))
		child := template("child", binding("root", root.Name))
		_, err := compiler(t, root, child).CompileAgentTemplate(context.Background(), harness, root)
		require.ErrorContains(t, err, "cycle")
	})
	t.Run("consecutive shared depth", func(t *testing.T) {
		root := template("root", binding("child", "child"))
		child := template("child", binding("grandchild", "grandchild"))
		grandchild := template("grandchild")
		_, err := compiler(t, child, grandchild).CompileAgentTemplate(context.Background(), harness, root)
		require.ErrorContains(t, err, "consecutive Shared")
	})
	t.Run("not admitted", func(t *testing.T) {
		root := template("root", binding("child", "child"))
		child := template("child")
		child.Labels = nil
		_, err := compiler(t, child).CompileAgentTemplate(context.Background(), harness, root)
		require.ErrorContains(t, err, "not admitted")
	})
	t.Run("dedicated", func(t *testing.T) {
		root := template("root", binding("child", "child"))
		root.Spec.Tools[0].Agent.Isolation = v1alpha3.AgentToolIsolationDedicated
		_, err := compiler(t).CompileAgentTemplate(context.Background(), harness, root)
		require.ErrorContains(t, err, "Dedicated")
	})
}

func TestCompileAgentTemplateKeepsSkillCredentialsOutOfConfig(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "skills-git-auth", Namespace: "test", UID: types.UID("secret-uid")},
		Data:       map[string][]byte{"token": []byte("ghp_private-skills-token")},
	}
	harness := &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{Name: "kagent", Namespace: "test"},
		Spec: v1alpha3.HarnessSpec{
			Kagent:                &v1alpha3.KagentHarness{},
			AllowedAgentTemplates: &v1alpha3.HarnessAgentTemplateAdmission{Selector: metav1.LabelSelector{}},
			Workload:              v1alpha3.HarnessWorkload{Image: "example.com/kagent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			Substrate: v1alpha3.HarnessSubstratePolicy{
				WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.HarnessSnapshotPolicy{Location: "snapshots"},
			},
		},
	}
	credentialRef := &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secret.Name}, Key: "token"}
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "helper", Namespace: "test"},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig: &corev1.LocalObjectReference{Name: "default-model"},
			Skills: []v1alpha3.AgentTemplateSkill{
				{Name: "runbooks", Source: v1alpha3.ArtifactSource{Git: &v1alpha3.GitArtifact{
					URL: "https://github.com/acme/private-skills", Commit: "cccccccccccccccccccccccccccccccccccccccc", CredentialRef: credentialRef,
				}, Path: "runbooks"}},
				{Name: "public", Source: v1alpha3.ArtifactSource{Git: &v1alpha3.GitArtifact{
					URL: "https://github.com/acme/public-skills", Commit: "dddddddddddddddddddddddddddddddddddddddd",
				}}},
			},
			Plugins: []v1alpha3.PluginBundle{{
				Source: v1alpha3.ArtifactSource{Git: &v1alpha3.GitArtifact{
					URL: "https://github.com/acme/private-plugins", Commit: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", CredentialRef: credentialRef,
				}},
				Skills: []string{"deploy"},
			}},
		},
	}
	spec, err := compiler(t, modelConfig(), secret).CompileAgentTemplate(context.Background(), harness, template)
	require.NoError(t, err)

	if bytes.Contains(spec.ConfigJSON, secret.Data["token"]) || bytes.Contains(spec.Provenance, secret.Data["token"]) {
		t.Fatal("runtime revision contains the skill credential value")
	}
	var config adk.AgentConfig
	require.NoError(t, json.Unmarshal(spec.ConfigJSON, &config))
	require.NotNil(t, config.AgentPlugins)
	require.Len(t, config.AgentPlugins.Skills, 2)
	require.Len(t, config.AgentPlugins.Plugins, 1)

	var artifactCredentials []corev1.EnvVar
	for _, variable := range spec.Environment {
		require.Nil(t, variable.ValueFrom, "runtime revision environment contains unresolved valueFrom: %+v", variable)
		if strings.HasPrefix(variable.Name, v2translator.ArtifactCredentialEnvPrefix) {
			artifactCredentials = append(artifactCredentials, variable)
		}
	}
	require.Len(t, artifactCredentials, 1, "one Secret key compiles to one variable")
	require.Equal(t, v2translator.CredentialPlaceholder, artifactCredentials[0].Value, "the actor holds a placeholder, the gateway the credential")
	require.Contains(t, spec.Credentials, egress.Credential{
		Hostname: "github.com", Header: "authorization", Prefix: "Basic ", URI: "ate-secret://kubernetes.io/test/" + secret.Name + "/token",
	}, "credentials: %+v", spec.Credentials)
	require.Zero(t, bytes.Count(spec.Provenance, []byte(`"kind":"Secret"`)), "a rotation of the credential is not a new revision; the gateway reads the Secret live: %s", spec.Provenance)
	require.Equal(t, []string{"api.openai.com", "github.com"}, spec.EgressDestinations)
}

func TestCompileAgentTemplateReportsAMissingSkillCredentialSecret(t *testing.T) {
	harness := &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{Name: "kagent", Namespace: "test"},
		Spec: v1alpha3.HarnessSpec{
			Kagent:                &v1alpha3.KagentHarness{},
			AllowedAgentTemplates: &v1alpha3.HarnessAgentTemplateAdmission{Selector: metav1.LabelSelector{}},
			Workload:              v1alpha3.HarnessWorkload{Image: "example.com/kagent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			Substrate: v1alpha3.HarnessSubstratePolicy{
				WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.HarnessSnapshotPolicy{Location: "snapshots"},
			},
		},
	}
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "helper", Namespace: "test"},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig: &corev1.LocalObjectReference{Name: "default-model"},
			Skills: []v1alpha3.AgentTemplateSkill{{Name: "runbooks", Source: v1alpha3.ArtifactSource{Git: &v1alpha3.GitArtifact{
				URL: "https://github.com/acme/private-skills", Commit: "cccccccccccccccccccccccccccccccccccccccc",
				CredentialRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "missing"}, Key: "token"},
			}}}},
		},
	}
	_, err := compiler(t, modelConfig()).CompileAgentTemplate(context.Background(), harness, template)
	require.ErrorContains(t, err, `secret "missing" not found`)
	var validation *v2translator.ValidationError
	require.False(t, errors.As(err, &validation), "a missing Secret is a reference failure the controller retries, not a validation error")
}
