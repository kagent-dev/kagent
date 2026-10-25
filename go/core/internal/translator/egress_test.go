package translator_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func egressPolicy() (*v1alpha3.EgressPolicy, *corev1.Secret) {
	return &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "tickets"}, Spec: v1alpha3.EgressPolicySpec{
		Destinations: []v1alpha3.EgressDestination{{Origin: "https://tickets.example.com", Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: new(corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "ticket-auth"}, Key: "token"})}}}}},
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
			auth := policy.Spec.Destinations[0].Headers
			auth[0].Name, auth[0].Prefix = tc.header, tc.prefix
			resolution := translator.CompileEgressPolicies(krt.TestingDummyContext{}, mockCollections(t, policy, secret), "test", []corev1.LocalObjectReference{{Name: policy.Name}})
			require.Empty(t, resolution.Issues)
			access := resolution.Access
			wantHeader := "authorization"
			if tc.header == "X-API-Key" {
				wantHeader = "x-api-key"
			}
			require.Equal(t, []egress.Credential{{Origin: "https://tickets.example.com:443", Header: wantHeader, Prefix: tc.want, URI: "ate-secret://k8s.io/default/test/ticket-auth/token"}}, access.Credentials)
		})
	}
}

func TestEgressPolicyHTTPCredentials(t *testing.T) {
	policy, secret := egressPolicy()
	policy.Spec.Destinations[0].Origin = "http://tickets.example.com:8080"
	resolution := translator.CompileEgressPolicies(krt.TestingDummyContext{}, mockCollections(t, policy, secret), "test", []corev1.LocalObjectReference{{Name: policy.Name}})
	require.Empty(t, resolution.Issues)
	access := resolution.Access
	require.Equal(t, []string{"http://tickets.example.com:8080"}, access.Destinations)
	require.Equal(t, []egress.Credential{{Origin: "http://tickets.example.com:8080", Header: "authorization", Prefix: "Bearer ", URI: "ate-secret://k8s.io/default/test/ticket-auth/token"}}, access.Credentials)
}

func TestAgentEgressPoliciesDoNotChangeRuntimeAcrossHarnesses(t *testing.T) {
	for _, kind := range []string{"kagent", "codex", "claude", "byo"} {
		t.Run(kind, func(t *testing.T) {
			policy, secret := egressPolicy()
			harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "runner"}, Spec: v1alpha3.HarnessSpec{
				Workload:  v1alpha3.HarnessWorkload{Image: new("runtime@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")},
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
				model.Spec.Headers = []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secret.Name}, Key: "token"}}}}
				model.Spec.OpenAI = &v1alpha3.OpenAIConfig{APIFormat: new(v1alpha3.OpenAIAPIFormatResponses)}
			}
			if kind == "claude" {
				model.Spec.Provider, model.Spec.Model = v1alpha3.ModelProviderAnthropic, "claude-sonnet-4-5"
				model.Spec.Headers = []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secret.Name}, Key: "token"}}}}
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
			var recorded struct{ Spec v1alpha3.AgentSpec }
			require.NoError(t, json.Unmarshal(result.Provenance, &recorded))
			require.Equal(t, agent.Spec, recorded.Spec, "record the full source spec, including live policy references")
			require.NotContains(t, result.EgressDestinations, "https://tickets.example.com:443")
			require.NotEmpty(t, result.EgressDestinations, "preserve inferred integration access")
			for _, binding := range result.Credentials {
				require.NotEqual(t, "https://tickets.example.com:443", binding.Origin)
			}
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
			changed := compile()
			updated, err := changed.Digest()
			require.NoError(t, err)
			require.Equal(t, original, updated)
			agent.Spec.EgressPolicyRefs = nil
			unreferenced, err := compile().Digest()
			require.NoError(t, err)
			require.Equal(t, original, unreferenced)
			agent.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: policy.Name}}
			require.NotContains(t, result.EgressDestinations, "https://registry.npmjs.org:443", "previous revision remains pinned")
			// Conflicting and missing policies are independent of runtime compilation.
			policy.Spec.Destinations[0].Origin = "https://api.openai.com"
			auth := policy.Spec.Destinations[0].Headers
			if kind == "claude" {
				policy.Spec.Destinations[0].Origin = "https://api.anthropic.com"
				auth[0].Name, auth[0].Prefix = "x-api-key", nil
			}
			auth[0].ValueFrom.SecretKeyRef.Key = "alternate"
			secret.Data["alternate"] = []byte("different-token")
			model.Spec.Headers = []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secret.Name}, Key: "token"}}}}
			_, err = compiler(t, model, harness, policy, secret).CompileAgent(t.Context(), agent)
			require.NoError(t, err)
			withConflict, err := compile().Digest()
			require.NoError(t, err)
			agent.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: "missing"}}
			missing, err := compile().Digest()
			require.NoError(t, err)
			require.Equal(t, withConflict, missing)
			// Codex and Claude reject caller-token passthrough independently.
			if kind == "claude" || kind == "codex" {
				return
			}
			model.Spec.Headers = []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: ""}, Key: ""}}}}
			model.Spec.Headers = []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{TokenPassthrough: &v1alpha3.TokenPassthrough{}}}}
			_, err = compiler(t, model, harness, policy, secret).CompileAgent(t.Context(), agent)
			require.NoError(t, err)
		})
	}
}

func TestEgressPolicyResolutionFailsClosed(t *testing.T) {
	for _, name := range []string{"missing policy", "other namespace", "missing secret", "missing key", "optional secret", "wildcard auth", "empty auth", "missing header", "protected header", "invalid prefix", "conflicting auth", "conflicting prefix"} {
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
				policy.Spec.Destinations[0].Headers[0].ValueFrom.SecretKeyRef.Optional = new(true)
			case "wildcard auth":
				policy.Spec.Destinations[0].Origin = "https://*.example.com"
			case "empty auth":
				policy.Spec.Destinations[0].Headers = []v1alpha3.Header{{}}
			case "missing header":
				policy.Spec.Destinations[0].Headers[0].Name = ""
			case "protected header":
				policy.Spec.Destinations[0].Headers[0].Name = "Host"
			case "invalid prefix":
				policy.Spec.Destinations[0].Headers[0].Prefix = new("Bearer\r\nInjected: ")
			case "conflicting auth", "conflicting prefix":
				other := policy.DeepCopy()
				other.Name = "other"
				if name == "conflicting prefix" {
					other.Spec.Destinations[0].Headers[0].Prefix = new("Token ")
				} else {
					other.Spec.Destinations[0].Headers[0].ValueFrom.SecretKeyRef.Key = "other"
					secret.Data["other"] = []byte("different")
				}
				objects = append(objects, other)
				refs = append(refs, corev1.LocalObjectReference{Name: other.Name})
			}
			resolution := translator.CompileEgressPolicies(krt.TestingDummyContext{}, mockCollections(t, objects...), "test", refs)
			if name == "conflicting auth" || name == "conflicting prefix" {
				require.Empty(t, resolution.Issues, "individually valid policies reach admission")
				_, err := egress.CanonicalAccess(resolution.Access.Destinations, resolution.Access.Credentials, nil)
				require.Error(t, err)
			} else {
				require.Len(t, resolution.Issues, 1)
				require.Empty(t, resolution.Access.Destinations)
			}
		})
	}
}

func TestEgressPoliciesMergeCredentialsAndOrigins(t *testing.T) {
	policy, secret := egressPolicy()
	other := policy.DeepCopy()
	other.Name = "registries"
	other.Spec.Destinations = append(other.Spec.Destinations, v1alpha3.EgressDestination{Origin: "https://*.npmjs.org"}, v1alpha3.EgressDestination{
		Origin: "https://tickets.example.com:8443", Headers: []v1alpha3.Header{{Name: "X-API-Key", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: policy.Spec.Destinations[0].Headers[0].ValueFrom.SecretKeyRef}}},
	})
	collections := mockCollections(t, policy, other, secret)
	refs := []corev1.LocalObjectReference{{Name: policy.Name}, {Name: other.Name}}
	resolution := translator.CompileEgressPolicies(krt.TestingDummyContext{}, collections, "test", refs)
	require.Empty(t, resolution.Issues)
	access, err := egress.CanonicalAccess(resolution.Access.Destinations, resolution.Access.Credentials, nil)
	require.NoError(t, err)
	require.Len(t, access.Destinations, 3)
	require.Len(t, access.Credentials, 2)
	require.Equal(t, "x-api-key", access.Credentials[1].Header)
	refs[0], refs[1] = refs[1], refs[0]
	reversed := translator.CompileEgressPolicies(krt.TestingDummyContext{}, collections, "test", refs)
	require.Empty(t, reversed.Issues)
	canonical, err := egress.CanonicalAccess(reversed.Access.Destinations, reversed.Access.Credentials, nil)
	require.NoError(t, err)
	require.Equal(t, access, canonical)
}

func TestEgressPoliciesResolveIndependently(t *testing.T) {
	policy, secret := egressPolicy()
	resolution := translator.CompileEgressPolicies(krt.TestingDummyContext{}, mockCollections(t, policy, secret), "test",
		[]corev1.LocalObjectReference{{Name: "z-missing"}, {Name: policy.Name}, {Name: "a-missing"}})
	require.Equal(t, []string{"https://tickets.example.com:443"}, resolution.Access.Destinations)
	require.Len(t, resolution.Access.Credentials, 1)
	require.Len(t, resolution.Issues, 2)
	require.Equal(t, "test/a-missing", resolution.Issues[0].Policy)
	require.Equal(t, v1alpha3.EgressPoliciesReasonNotFound, resolution.Issues[0].Reason)
	require.Equal(t, "test/z-missing", resolution.Issues[1].Policy)
}

func TestInvalidEgressReferencesRevokeAll(t *testing.T) {
	for _, name := range []string{"too many", "invalid name"} {
		t.Run(name, func(t *testing.T) {
			policy, secret := egressPolicy()
			refs := []corev1.LocalObjectReference{{Name: policy.Name}}
			if name == "invalid name" {
				refs = append(refs, corev1.LocalObjectReference{Name: "Invalid!"})
			} else {
				for range 16 {
					refs = append(refs, corev1.LocalObjectReference{Name: policy.Name})
				}
			}
			resolution := translator.CompileEgressPolicies(krt.TestingDummyContext{}, mockCollections(t, policy, secret), "test", refs)
			require.Empty(t, resolution.Access.Destinations)
			require.Empty(t, resolution.Access.Credentials)
			require.Len(t, resolution.Issues, len(refs))
			for _, issue := range resolution.Issues {
				require.Equal(t, v1alpha3.EgressPoliciesReasonInvalidEgressPolicy, issue.Reason)
			}
		})
	}
}
