package translator_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	legacy "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
	v2translator "github.com/kagent-dev/kagent/go/core/v2/translator"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	schemev1 "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func modelConfig() *v1alpha3.ModelConfig {
	return &v1alpha3.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "default-model", Namespace: "test"},
		Spec:       v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-4o"},
	}
}

func remoteMCPServer(name, url string) *v1alpha3.RemoteMCPServer {
	return &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test"}, Spec: v1alpha3.RemoteMCPServerSpec{
		URL: url, Protocol: v1alpha3.RemoteMCPServerProtocolStreamableHttp,
	}}
}

func compiler(t *testing.T, objects ...client.Object) *v2translator.Compiler {
	t.Helper()
	require.NoError(t, v1alpha3.AddToScheme(schemev1.Scheme))
	kube := fake.NewClientBuilder().WithScheme(schemev1.Scheme).WithObjects(objects...).Build()
	base := legacy.NewAdkApiTranslator(kube, types.NamespacedName{Name: "default-model", Namespace: "test"}, nil, "", nil)
	return v2translator.NewCompiler(kube, base, false)
}

func TestCompileAgentTemplateKeepsCredentialsOutOfRevision(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "mcp-auth", Namespace: "test"},
		Data:       map[string][]byte{"token": []byte("Bearer top-secret")},
	}
	server := remoteMCPServer("remote", "https://mcp.example.com/mcp")
	server.Spec.HeadersFrom = []v1alpha3.ValueRef{{
		Name: "Authorization",
		ValueFrom: &v1alpha3.ValueSource{
			Type: v1alpha3.SecretValueSource, Name: secret.Name, Key: "token",
		},
	}}
	harness := &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{Name: "kagent", Namespace: "test"},
		Spec: v1alpha3.HarnessSpec{
			Kagent:   &v1alpha3.KagentHarness{},
			Workload: v1alpha3.HarnessWorkload{Image: "example.com/kagent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			Substrate: v1alpha3.HarnessSubstratePolicy{
				WorkerPoolRef:  corev1.LocalObjectReference{Name: "default"},
				SnapshotPolicy: v1alpha3.HarnessSnapshotPolicy{Location: "snapshots"},
			},
		},
	}
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "helper", Namespace: "test"},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  v1alpha3.AgentTemplateLocalReference{Name: "default-model"},
			SystemPrompt: "help",
			Tools: []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{
				Server: v1alpha3.AgentTemplateTypedLocalReference{Kind: "RemoteMCPServer", Name: server.Name},
				Tools:  []string{"lookup"},
			}}},
		},
	}
	compiler := compiler(t, modelConfig(), server, secret)
	spec, err := compiler.CompileAgentTemplate(context.Background(), harness, template)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(spec.ConfigJSON, secret.Data["token"]) || bytes.Contains(spec.SourceSnapshot, secret.Data["token"]) {
		t.Fatal("runtime revision contains credential value")
	}
	if !bytes.Contains(spec.ConfigJSON, []byte("__KAGENT_ENV[KAGENT_CREDENTIAL_")) {
		t.Fatalf("config does not contain credential placeholder: %s", spec.ConfigJSON)
	}
	var foundSecretRef bool
	for _, variable := range spec.Environment {
		if variable.ValueFrom != nil && variable.ValueFrom.SecretKeyRef != nil && variable.ValueFrom.SecretKeyRef.Name == secret.Name {
			foundSecretRef = true
		}
	}
	if !foundSecretRef {
		t.Fatal("runtime revision environment does not preserve SecretKeyRef")
	}
	if len(spec.EgressDestinations) != 2 || spec.EgressDestinations[0] != "api.openai.com" || spec.EgressDestinations[1] != "mcp.example.com" {
		t.Fatalf("egress destinations = %v", spec.EgressDestinations)
	}
}

func TestRemoteMCPServerDoesNotUseLegacyProxy(t *testing.T) {
	rewrite := "http://proxy.test:8080"
	server := remoteMCPServer("remote", "http://remote.test.svc.cluster.local/mcp")
	require.NoError(t, v1alpha3.AddToScheme(schemev1.Scheme))
	kube := fake.NewClientBuilder().WithScheme(schemev1.Scheme).WithObjects(modelConfig(), server).Build()
	base := legacy.NewAdkApiTranslator(kube, types.NamespacedName{Name: "default-model", Namespace: "test"}, nil, rewrite, nil)
	runtimeModel, err := base.ResolveRuntimeModel(context.Background(), "test", "default-model")
	require.NoError(t, err)
	config := &adk.AgentConfig{}
	require.NoError(t, base.AddRemoteMCPServer(context.Background(), config, runtimeModel, server, &v1alpha3.McpServerTool{}, nil, false))
	require.Equal(t, server.Spec.URL, config.HttpTools[0].Params.Url)
}
