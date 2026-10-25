package translator_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
)

func TestAppliedIntegrationResolution(t *testing.T) {
	for _, kind := range []string{egress.IntegrationModelConfig, egress.IntegrationRemoteMCPServer} {
		for _, test := range []struct {
			name    string
			allowed bool
			issue   bool
		}{
			{name: "unchanged", allowed: true},
			{name: "deleted"},
			{name: "replaced"},
			{name: "removed headers"},
			{name: "changed binding"},
			{name: "invalid headers", allowed: true, issue: true},
			{name: "revoked then invalid", issue: true},
		} {
			t.Run(kind+"/"+test.name, func(t *testing.T) {
				target := egresspolicy.GatewayTarget{Origin: "https://api.openai.com:443", Header: "authorization", Prefix: "Bearer "}
				grant := egress.IntegrationGrant{Kind: kind, Name: "integration", UID: "applied-uid", Targets: []egresspolicy.GatewayTarget{target}}
				metadata := metav1.ObjectMeta{Namespace: "test", Name: grant.Name, UID: "applied-uid"}
				headers := []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{TokenPassthrough: &v1alpha3.TokenPassthrough{}}}}
				switch test.name {
				case "replaced":
					metadata.UID = "replacement"
				case "removed headers":
					headers = nil
				case "changed binding":
					headers[0].Prefix = new("Token ")
				case "invalid headers", "revoked then invalid":
					headers[0].Prefix = new("invalid\r\n")
				}
				if test.name == "revoked then invalid" {
					grant.RevokedTargets = grant.Targets
				}
				var objects []any
				if test.name != "deleted" {
					if kind == egress.IntegrationModelConfig {
						objects = append(objects, &v1alpha3.ModelConfig{ObjectMeta: metadata, Spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Headers: headers}})
					} else {
						objects = append(objects, &v1alpha3.RemoteMCPServer{ObjectMeta: metadata, Spec: v1alpha3.RemoteMCPServerSpec{URL: "https://api.openai.com/mcp", Headers: headers}})
					}
				}
				resolved, _ := translator.ResolveIntegrationGrants(krt.TestingDummyContext{}, mockCollections(t, objects...), "test", []egress.IntegrationGrant{grant})
				if test.allowed {
					require.Equal(t, []egresspolicy.GatewayTarget{target}, resolved.Access.TokenPassthroughTargets)
				} else {
					require.Empty(t, resolved.Access.TokenPassthroughTargets)
				}
				require.Equal(t, test.issue, len(resolved.Issues) > 0)
			})
		}
	}
}

func TestAuxiliaryModelIntegrationGrants(t *testing.T) {
	auxiliary := func(name string) *v1alpha3.ModelConfig {
		model := modelConfig()
		model.Name, model.UID = name, types.UID(name+"-uid")
		model.Spec.OpenAI = &v1alpha3.OpenAIConfig{BaseURL: fmt.Sprintf("https://%s.example/v1", name)}
		model.Spec.Headers = []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{TokenPassthrough: &v1alpha3.TokenPassthrough{}}}}
		return model
	}
	summary, embedding := auxiliary("summary"), auxiliary("embedding")
	harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Name: "kagent", Namespace: "test"}, Spec: v1alpha3.HarnessSpec{
		Kagent: &v1alpha3.KagentHarness{
			Compaction: &v1alpha3.KagentHarnessCompaction{CompactionInterval: new(5), Summarizer: &v1alpha3.KagentHarnessSummarizer{ModelConfigRef: &corev1.LocalObjectReference{Name: summary.Name}}},
			Memory:     &v1alpha3.KagentHarnessMemory{ModelConfigRef: corev1.LocalObjectReference{Name: embedding.Name}},
		},
		Substrate: v1alpha3.RuntimeSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}},
	}}
	template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "test"}, Spec: v1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: "default-model"}}}
	compiled, err := compiler(t, modelConfig(), summary, embedding, harness).CompileAgent(t.Context(), inlineAgent(harness, template))
	require.NoError(t, err)
	require.Len(t, compiled.IntegrationGrants, 2)
	for _, source := range []*v1alpha3.ModelConfig{summary, embedding} {
		require.Contains(t, compiled.IntegrationGrants, egress.IntegrationGrant{Kind: egress.IntegrationModelConfig, Name: source.Name, UID: string(source.UID), Targets: []egresspolicy.GatewayTarget{{Origin: "https://" + source.Name + ".example:443", Header: "authorization", Prefix: "Bearer "}}})
	}
	base := egress.Access{Destinations: compiled.EgressDestinations, Credentials: compiled.Credentials, TokenPassthroughTargets: compiled.TokenPassthroughTargets}
	for _, state := range []string{"present", "deleted", "replaced"} {
		t.Run(state, func(t *testing.T) {
			sources := []any{summary}
			if state != "deleted" {
				current := embedding.DeepCopy()
				if state == "replaced" {
					current.UID = "new-uid"
				}
				sources = append(sources, current)
			}
			resolved, _ := translator.ResolveIntegrationGrants(krt.TestingDummyContext{}, mockCollections(t, sources...), "test", compiled.IntegrationGrants)
			effective, err := egress.EffectiveAccess(base, resolved.Access)
			require.NoError(t, err)
			require.Contains(t, effective.TokenPassthroughTargets, egresspolicy.GatewayTarget{Origin: "https://summary.example:443", Header: "authorization", Prefix: "Bearer "})
			target := egresspolicy.GatewayTarget{Origin: "https://embedding.example:443", Header: "authorization", Prefix: "Bearer "}
			if state == "present" {
				require.Contains(t, effective.TokenPassthroughTargets, target)
			} else {
				require.NotContains(t, effective.TokenPassthroughTargets, target)
			}
		})
	}
}
