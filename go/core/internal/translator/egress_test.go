package translator_test

import (
	"encoding/json"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func egressPolicy() (*v1alpha3.EgressPolicy, *corev1.Secret) {
	return &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "tickets"}, Spec: v1alpha3.EgressPolicySpec{
		Destinations: []v1alpha3.EgressDestination{{Origin: "https://tickets.example.com", Authentication: &v1alpha3.EgressAuthentication{
			Header: "Authorization", Prefix: new("Bearer "), SecretKeyRef: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "ticket-auth"}, Key: "token"},
		}}},
	}}, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "ticket-auth"}, Data: map[string][]byte{"token": []byte("private-ticket-token")}}
}

func TestEgressPolicyCredentialPrefixes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		header string
		prefix *string
		want   string
	}{
		{name: "bearer token", header: "Authorization", prefix: new("Bearer "), want: "Bearer "},
		{name: "API key", header: "X-API-Key"},
		{name: "empty prefix", header: "X-API-Key", prefix: new("")},
		{name: "custom prefix", header: "Authorization", prefix: new("Token "), want: "Token "},
		{name: "literal prefix", header: "Authorization", prefix: new("Token"), want: "Token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy, secret := egressPolicy()
			auth := policy.Spec.Destinations[0].Authentication
			auth.Header, auth.Prefix = tc.header, tc.prefix
			access, err := translator.CompileEgressPolicies(krt.TestingDummyContext{}, mockCollections(t, policy, secret), "test", []corev1.LocalObjectReference{{Name: policy.Name}})
			require.NoError(t, err)
			wantHeader := "authorization"
			if tc.header == "X-API-Key" {
				wantHeader = "x-api-key"
			}
			require.Equal(t, []egress.Credential{{Origin: "https://tickets.example.com:443", Header: wantHeader, Prefix: tc.want, URI: "ate-secret://k8s.io/default/test/ticket-auth/token"}}, access.Credentials)
		})
	}
}

func TestAgentEgressPoliciesPinGrantsAcrossHarnesses(t *testing.T) {
	for _, kind := range []string{"kagent", "codex", "claude", "byo"} {
		t.Run(kind, func(t *testing.T) {
			policy, secret := egressPolicy()
			harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "runner"}, Spec: v1alpha3.HarnessSpec{
				Workload:  v1alpha3.HarnessWorkload{Image: "runtime@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
				Substrate: v1alpha3.RuntimeSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"}},
			}}
			switch kind {
			case "kagent":
				harness.Spec.Kagent = &v1alpha3.KagentHarness{}
			case "codex":
				harness.Spec.Codex = &v1alpha3.CodexHarness{}
			case "claude":
				harness.Spec.Claude = &v1alpha3.ClaudeHarness{}
			case "byo":
				harness.Spec.BYO = &v1alpha3.BYOHarness{}
			}
			model := modelConfig()
			if kind == "codex" {
				model.Spec.APIKeySecret, model.Spec.APIKeySecretKey = secret.Name, "token"
				model.Spec.OpenAI = &v1alpha3.OpenAIConfig{APIFormat: new(v1alpha3.OpenAIAPIFormatResponses)}
			}
			if kind == "claude" {
				model.Spec.Provider, model.Spec.Model = v1alpha3.ModelProviderAnthropic, "claude-sonnet-4-5"
				model.Spec.APIKeySecret, model.Spec.APIKeySecretKey = secret.Name, "token"
			}
			template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "helper"}, Spec: v1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: model.Name}, SystemPrompt: "help"}}
			agent := inlineAgent(harness, template)
			agent.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: policy.Name}}
			compile := func() *translator.CompileResult {
				result, err := compiler(t, model, harness, policy, secret).CompileAgent(t.Context(), agent)
				require.NoError(t, err)
				return result
			}
			result := compile()
			require.Contains(t, result.EgressDestinations, "https://tickets.example.com:443")
			require.Greater(t, len(result.EgressDestinations), 1, "explicit policies preserve inferred destinations")
			require.Equal(t, "https://tickets.example.com:443", result.Credentials[len(result.Credentials)-1].Origin)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), string(secret.Data["token"]))
			original, err := result.Digest()
			require.NoError(t, err)
			secret.Data["token"], secret.UID = []byte("rotated-token"), "replacement"
			rotated, err := compile().Digest()
			require.NoError(t, err)
			require.Equal(t, original, rotated)
			policy.Spec.Destinations = append(policy.Spec.Destinations, v1alpha3.EgressDestination{Origin: "https://registry.npmjs.org"})
			updated, err := compile().Digest()
			require.NoError(t, err)
			require.NotEqual(t, original, updated)
			require.NotContains(t, result.EgressDestinations, "https://registry.npmjs.org:443", "previous revision remains pinned")
			// Explicit policy credentials cannot silently replace inferred model credentials.
			policy.Spec.Destinations[0].Origin = "https://api.openai.com"
			auth := policy.Spec.Destinations[0].Authentication
			if kind == "claude" {
				policy.Spec.Destinations[0].Origin = "https://api.anthropic.com"
				auth.Header, auth.Prefix = "x-api-key", nil
			}
			auth.SecretKeyRef.Key = "alternate"
			secret.Data["alternate"] = []byte("different-token")
			model.Spec.APIKeySecret, model.Spec.APIKeySecretKey = secret.Name, "token"
			_, err = compiler(t, model, harness, policy, secret).CompileAgent(t.Context(), agent)
			require.ErrorContains(t, err, "conflicting credentials")
			// Codex and Claude reject caller-token passthrough independently.
			if kind == "claude" || kind == "codex" {
				return
			}
			model.Spec.APIKeySecret, model.Spec.APIKeySecretKey = "", ""
			model.Spec.APIKeyPassthrough = true
			_, err = compiler(t, model, harness, policy, secret).CompileAgent(t.Context(), agent)
			require.ErrorContains(t, err, "cannot combine caller-token passthrough")
		})
	}
}

func TestEgressPolicyResolutionFailsClosed(t *testing.T) {
	for _, name := range []string{"missing policy", "other namespace", "missing secret", "missing key", "optional secret", "HTTP auth", "wildcard auth", "empty auth", "missing header", "protected header", "invalid prefix", "conflicting auth", "conflicting prefix"} {
		t.Run(name, func(t *testing.T) {
			policy, secret := egressPolicy()
			objects := []any{policy, secret}
			refs := []corev1.LocalObjectReference{{Name: policy.Name}}
			switch name {
			case "missing policy":
				objects = []any{secret}
			case "other namespace":
				policy.Namespace = "other"
			case "missing secret":
				objects = []any{policy}
			case "missing key":
				secret.Data = nil
			case "optional secret":
				policy.Spec.Destinations[0].Authentication.SecretKeyRef.Optional = new(true)
			case "HTTP auth":
				policy.Spec.Destinations[0].Origin = "http://tickets.example.com"
			case "wildcard auth":
				policy.Spec.Destinations[0].Origin = "https://*.example.com"
			case "empty auth":
				policy.Spec.Destinations[0].Authentication = &v1alpha3.EgressAuthentication{}
			case "missing header":
				policy.Spec.Destinations[0].Authentication.Header = ""
			case "protected header":
				policy.Spec.Destinations[0].Authentication.Header = "Host"
			case "invalid prefix":
				policy.Spec.Destinations[0].Authentication.Prefix = new("Bearer\r\nInjected: ")
			case "conflicting auth", "conflicting prefix":
				other := policy.DeepCopy()
				other.Name = "other"
				if name == "conflicting prefix" {
					other.Spec.Destinations[0].Authentication.Prefix = new("Token ")
				} else {
					other.Spec.Destinations[0].Authentication.SecretKeyRef.Key = "other"
					secret.Data["other"] = []byte("different")
				}
				objects = append(objects, other)
				refs = append(refs, corev1.LocalObjectReference{Name: other.Name})
			}
			_, err := translator.CompileEgressPolicies(krt.TestingDummyContext{}, mockCollections(t, objects...), "test", refs)
			require.Error(t, err)
		})
	}
}

func TestEgressPoliciesMergeCredentialsAndOrigins(t *testing.T) {
	policy, secret := egressPolicy()
	other := policy.DeepCopy()
	other.Name = "registries"
	other.Spec.Destinations = append(other.Spec.Destinations, v1alpha3.EgressDestination{Origin: "https://*.npmjs.org"}, v1alpha3.EgressDestination{
		Origin: "https://tickets.example.com:8443", Authentication: &v1alpha3.EgressAuthentication{Header: "X-API-Key", SecretKeyRef: policy.Spec.Destinations[0].Authentication.SecretKeyRef},
	})
	collections := mockCollections(t, policy, other, secret)
	refs := []corev1.LocalObjectReference{{Name: policy.Name}, {Name: other.Name}}
	access, err := translator.CompileEgressPolicies(krt.TestingDummyContext{}, collections, "test", refs)
	require.NoError(t, err)
	require.Len(t, access.Destinations, 3)
	require.Len(t, access.Credentials, 2)
	require.Equal(t, "x-api-key", access.Credentials[1].Header)
	refs[0], refs[1] = refs[1], refs[0]
	reversed, err := translator.CompileEgressPolicies(krt.TestingDummyContext{}, collections, "test", refs)
	require.NoError(t, err)
	require.Equal(t, access, reversed)
}
