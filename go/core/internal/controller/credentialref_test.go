package controller

import (
	"strings"
	"testing"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/prototext"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// A Harness credentialRef must resolve before a revision is compiled, and the
// resolved Secret value must never reach the ActorTemplate whose golden
// snapshot every session of the agent shares.
func TestReconciliationHarnessCredentialRefResolvedRefs(t *testing.T) {
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	opts := krt.NewOptionsBuilder(stop, "test-credential-ref", nil)

	template := &kagentv1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "assistant", UID: "template-uid"},
		Spec:       kagentv1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: "model"}, SystemPrompt: "help"},
	}
	runtimeHarness := harness("team-a", "kagent", nil)
	runtimeHarness.UID = "harness-uid"
	runtimeHarness.Spec.Kagent = &kagentv1alpha3.KagentHarness{}
	runtimeHarness.Spec.Workload.Image = "example.com/kagent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	runtimeHarness.Spec.Substrate = kagentv1alpha3.RuntimeSubstratePolicy{
		WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: kagentv1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"},
	}
	runtimeHarness.Spec.Env = []kagentv1alpha3.RuntimeEnvVar{{Name: "TICKETS_API_TOKEN", CredentialRef: &kagentv1alpha3.RuntimeCredentialRef{
		Name: "tickets-auth", Key: "token", URL: "https://tickets.example.com/api", Header: "Authorization", Prefix: "Bearer ",
	}}}
	model := &kagentv1alpha3.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "model"},
		Spec:       kagentv1alpha3.ModelConfigSpec{Provider: kagentv1alpha3.ModelProviderOpenAI, Model: "gpt-5"},
	}
	mock := krttest.NewMock(t, []any{template, runtimeHarness, model, &atev1alpha1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "default"}}})
	secrets := krt.NewStaticCollection[*corev1.Secret](nil, nil, opts.WithName("Secrets")...)
	configMaps := krttest.GetMockCollection[*corev1.ConfigMap](mock)
	_, resolvedModels := newModelConfigReconciliations(krttest.GetMockCollection[*kagentv1alpha3.ModelConfig](mock), configMaps, secrets, opts)
	agents := krt.NewStaticCollection(nil, []*kagentv1alpha3.Agent{testAgent(template, runtimeHarness)}, opts.WithName("Agents")...)
	reconciliations := newAgentReconciliations(agents, v2translator.Collections{
		Harnesses: krttest.GetMockCollection[*kagentv1alpha3.Harness](mock), AgentTemplates: krttest.GetMockCollection[*kagentv1alpha3.AgentTemplate](mock),
		ResolvedModelConfigs: resolvedModels, RemoteMCPServers: krttest.GetMockCollection[*kagentv1alpha3.RemoteMCPServer](mock),
		ConfigMaps: configMaps, Secrets: secrets, WorkerPools: krttest.GetMockCollection[*atev1alpha1.WorkerPool](mock),
	}, krt.NewStaticCollection[AgentRuntimeObservation](nil, nil, opts.WithName("AgentRuntimeObservations")...), opts)
	statuses := newAgentStatuses(agents, reconciliations, opts)

	resolvedRefsFailure := func(message string) func() bool {
		return func() bool {
			updates := statuses.List()
			if len(updates) != 1 {
				return false
			}
			condition := apimeta.FindStatusCondition(updates[0].Status.Conditions, kagentv1alpha3.AgentConditionResolvedRefs)
			return condition != nil && condition.Status == metav1.ConditionFalse && condition.Reason == "ReferenceResolutionFailed" && strings.Contains(condition.Message, message)
		}
	}
	waitFor(t, resolvedRefsFailure(`secret "tickets-auth" not found`))

	secrets.UpdateObject(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "tickets-auth"}, Data: map[string][]byte{"other": []byte("x")}})
	waitFor(t, resolvedRefsFailure(`secret "tickets-auth" does not contain key "token"`))

	const value = "tickets-secret-value"
	secrets.UpdateObject(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "tickets-auth"}, Data: map[string][]byte{"token": []byte(value)}})
	key := "team-a/assistant"
	waitFor(t, func() bool {
		state := reconciliations.GetKey(key)
		return state != nil && state.CompilationFailure == nil && state.Target != nil
	})
	waitFor(t, func() bool {
		updates := statuses.List()
		if len(updates) != 1 {
			return false
		}
		condition := apimeta.FindStatusCondition(updates[0].Status.Conditions, kagentv1alpha3.AgentConditionResolvedRefs)
		return condition != nil && condition.Status == metav1.ConditionTrue
	})
	target := reconciliations.GetKey(key).Target
	require.NotContains(t, prototext.Format(target.ActorTemplate), value, "the golden-snapshotted ActorTemplate must not contain the Secret value")
	require.Contains(t, target.Revision.Credentials, egress.Credential{Hostname: "tickets.example.com", Header: "authorization", Prefix: "Bearer ", URI: "ate-secret://k8s.io/default/team-a/tickets-auth/token"})
	require.Contains(t, target.Revision.EgressDestinations, "https://tickets.example.com:443")
}
