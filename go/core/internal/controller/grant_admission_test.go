package controller

import (
	"strings"
	"testing"
	"testing/synctest"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kagentfake "github.com/kagent-dev/kagent/go/api/clientset/versioned/fake"
	v1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func TestGrantAdmissionAcrossSources(t *testing.T) {
	for _, kind := range []string{"agent", "sandbox"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				opts := krt.NewOptionsBuilder(t.Context().Done(), "admission", nil)
				var policies krt.StaticCollection[*v1alpha3.EgressPolicy]
				var reconcile func() error
				var stored func() egress.Access
				var conditions func() []metav1.Condition
				switch kind {
				case "agent":
					collections, _ := newPreparationTestCollections(t, "gvisor")
					initial := collections.Reconciliations.List()[0]
					agent := initial.Agent.DeepCopy()
					agent.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: "a"}, {Name: "b"}}
					collections.Agents.(krt.StaticCollection[*v1alpha3.Agent]).UpdateObject(agent)
					policies = collections.EgressPolicies.(krt.StaticCollection[*v1alpha3.EgressPolicy])
					template := proto.CloneOf(initial.Target.ActorTemplate)
					template.Metadata.Uid, template.Status = "prepared", sandboxGoldenStatus()
					store := &fakeRuntimeRevisionStore{}
					r := &Reconciler{collections: collections, templates: &fakeActorTemplates{template: template}, store: store}
					reconcile = func() error { return r.reconcileAgent(t.Context(), initial.ResourceName()) }
					stored = func() egress.Access { return store.pair.EgressGrant }
					conditions = func() []metav1.Condition {
						state := collections.Reconciliations.GetKey(initial.ResourceName())
						require.NotNil(t, state.Target, "grant conflicts must not block compilation")
						require.Equal(t, initial.Target.RevisionID, state.Target.RevisionID)
						return statusForAgent(*state, agent.Generation, "").Conditions
					}
				case "sandbox":
					template := sandboxTestTemplate()
					template.Finalizers = []string{sandboxPreparationFinalizer}
					template.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: "a"}, {Name: "b"}}
					templates := krt.NewStaticCollection(nil, []*v1alpha3.SandboxTemplate{template}, opts.WithName("templates")...)
					policies = krt.NewStaticCollection[*v1alpha3.EgressPolicy](nil, nil, opts.WithName("policies")...)
					pools := krt.NewStaticCollection(nil, []*atev1alpha1.WorkerPool{{ObjectMeta: metav1.ObjectMeta{Namespace: template.Namespace, Name: "default"}}}, opts.WithName("pools")...)
					store := &sandboxTestStore{}
					actors := &sandboxTestActors{store: store, templates: map[string]*ateapipb.ActorTemplate{}, autoReady: true}
					s := &SandboxReconciler{collections: newSandboxCollections(Collections{SandboxTemplates: templates, WorkerPools: pools, EgressPolicies: policies}, substrate.SandboxPolicy{GuestImage: "guest@sha256:" + strings.Repeat("b", 64), CPU: "1", Memory: "1Gi"}, opts),
						store: store, actors: actors, client: kagentfake.NewSimpleClientset(template).ApiV1alpha3()}
					reconcile = func() error { return s.reconcile(t.Context(), "team-a/scratch") }
					stored = func() egress.Access { return store.desired.EgressGrant }
					conditions = func() []metav1.Condition {
						state := s.collections.states.GetKey("team-a/scratch")
						require.NotNil(t, state.DesiredActorTemplate)
						return sandboxStatusWithTransitionTimes(*state).Conditions
					}
				}
				policy := func(name string, origins ...string) *v1alpha3.EgressPolicy {
					p := &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: name}}
					for _, origin := range origins {
						p.Spec.Destinations = append(p.Spec.Destinations, v1alpha3.EgressDestination{Origin: origin})
					}
					return p
				}
				check := func(reason string, origins ...string) {
					t.Helper()
					synctest.Wait()
					require.NoError(t, reconcile())
					synctest.Wait()
					require.Equal(t, origins, stored().Destinations)
					observed := conditions()
					require.True(t, meta.IsStatusConditionTrue(observed, "Ready"), "%v", observed)
					condition := meta.FindStatusCondition(observed, "EgressPoliciesApplied")
					require.NotNil(t, condition)
					require.Equal(t, reason, condition.Reason)
					if reason == v1alpha3.EgressPoliciesReasonConflict {
						require.Contains(t, condition.Message, "new grants are held until the conflict is resolved; first conflict:")
					}
					if reason == v1alpha3.EgressPoliciesReasonApplied {
						require.Equal(t, metav1.ConditionTrue, condition.Status)
					} else {
						require.Equal(t, metav1.ConditionFalse, condition.Status)
					}
				}
				const a = "https://a.example.com:443"
				const x = "https://x.example.com:443"
				policies.UpdateObject(policy("a", a))
				policies.UpdateObject(policy("b", "http://a.example.com:443"))
				check(v1alpha3.EgressPoliciesReasonConflict)
				policies.UpdateObject(policy("b", x))
				check(v1alpha3.EgressPoliciesReasonApplied, a, x)
				// The new HTTP rule conflicts with a. An unrelated addition is held
				// too, and the removed x is revoked immediately.
				policies.UpdateObject(policy("b", "http://a.example.com:443", "https://new.example.com"))
				check(v1alpha3.EgressPoliciesReasonConflict, a)
				// Repeating preparation cannot admit previously held additions.
				check(v1alpha3.EgressPoliciesReasonConflict, a)
				policies.DeleteObject("team-a/b")
				check(v1alpha3.EgressPoliciesReasonNotFound, a)
				policies.UpdateObject(policy("b", "https://new.example.com"))
				check(v1alpha3.EgressPoliciesReasonApplied, a, "https://new.example.com:443")
			})
		})
	}
}

func TestGrantAdmissionWithChangedBase(t *testing.T) {
	const a = "https://a.example.com:443"
	const model = "https://model.example.com:443"
	old := egress.Access{Destinations: []string{a, model}}
	base := egress.Access{Destinations: []string{"http://model.example.com:443"}}
	candidate, issues, conflict := evaluateEgressGrant(translator.EgressGrantResolution{Access: old}, base, nil)
	require.True(t, conflict)
	require.Len(t, issues, 1)
	require.Equal(t, egress.Access{Destinations: []string{a}}, retainedEgressGrant(old, candidate, base, nil))
	// With no compiled target, admission can still revoke grants safely.
	require.Equal(t, old, retainedEgressGrant(old, candidate, egress.Access{}, nil))
	require.Empty(t, retainedEgressGrant(egress.Access{}, candidate, base, nil).Destinations, "new identities inherit no grants")
}

func TestEgressConditionSurvivesRuntimeFailureAndBoundsMessages(t *testing.T) {
	issues := []translator.EgressGrantIssue{
		{Policy: "team/missing", Reason: v1alpha3.EgressPoliciesReasonNotFound, Message: "missing"},
		{Reason: v1alpha3.EgressPoliciesReasonConflict, Message: strings.Repeat("conflict", 1000)},
	}
	state := AgentReconciliation{EgressIssues: issues, EgressConflict: true,
		CompilationFailure: &ReconciliationFailure{Condition: v1alpha3.AgentConditionResolvedRefs, Reason: "ReferenceResolutionFailed", Message: "Harness is missing"}}
	status := statusForAgent(state, 1, "")
	condition := meta.FindStatusCondition(status.Conditions, v1alpha3.AgentConditionEgressPoliciesApplied)
	require.Equal(t, v1alpha3.EgressPoliciesReasonConflict, condition.Reason)
	require.Contains(t, condition.Message, "new grants are held until the conflict is resolved")
	require.Contains(t, condition.Message, "team/missing")
	require.LessOrEqual(t, len(condition.Message), 4096)
	require.False(t, meta.IsStatusConditionTrue(status.Conditions, v1alpha3.AgentConditionReady))
}
