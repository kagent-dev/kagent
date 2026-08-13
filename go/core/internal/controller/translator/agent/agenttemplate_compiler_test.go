package agent_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCompileAgentTemplateKeepsCredentialsOutOfPreparedConfig(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "mcp-auth", Namespace: "test"},
		Data:       map[string][]byte{"token": []byte("Bearer top-secret")},
	}
	server := egressRMS("remote", "https://mcp.example.com/mcp")
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
	translator := egressTranslator(t, false, "", egressModelConfig(), server, secret)
	bundle, err := translator.CompileAgentTemplate(context.Background(), harness, template)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(bundle.ConfigJSON, secret.Data["token"]) || bytes.Contains(bundle.SourceSnapshot, secret.Data["token"]) {
		t.Fatal("prepared data contains credential value")
	}
	if !bytes.Contains(bundle.ConfigJSON, []byte("__KAGENT_ENV[KAGENT_CREDENTIAL_")) {
		t.Fatalf("config does not contain credential placeholder: %s", bundle.ConfigJSON)
	}
	var foundSecretRef bool
	for _, variable := range bundle.Environment {
		if variable.ValueFrom != nil && variable.ValueFrom.SecretKeyRef != nil && variable.ValueFrom.SecretKeyRef.Name == secret.Name {
			foundSecretRef = true
		}
	}
	if !foundSecretRef {
		t.Fatal("prepared environment does not preserve SecretKeyRef")
	}
	if len(bundle.EgressDestinations) != 2 || bundle.EgressDestinations[0] != "api.openai.com" || bundle.EgressDestinations[1] != "mcp.example.com" {
		t.Fatalf("egress destinations = %v", bundle.EgressDestinations)
	}
}
