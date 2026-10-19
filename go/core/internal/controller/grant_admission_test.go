package controller

import (
	"strings"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	atev1alpha1 "go.ate.dev/substrate/pkg/api/v1alpha1"
	"go.ate.dev/substrate/pkg/proto/ateapipb"
	"google.golang.org/protobuf/proto"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kagentfake "github.com/kagent-dev/kagent/go/api/clientset/versioned/fake"
	v1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
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

func TestAgentGrantAdmissionWithAndWithoutCompiledBase(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		collections, harnesses := newPreparationTestCollections(t, "gvisor")
		initial := collections.Reconciliations.List()[0]
		template := proto.CloneOf(initial.Target.ActorTemplate)
		template.Metadata.Uid, template.Status = "prepared", sandboxGoldenStatus()
		store := &fakeRuntimeRevisionStore{}
		r := &Reconciler{collections: collections, templates: &fakeActorTemplates{template: template}, store: store}
		policies := collections.EgressPolicies.(krt.StaticCollection[*v1alpha3.EgressPolicy])
		agents := collections.Agents.(krt.StaticCollection[*v1alpha3.Agent])
		agent := initial.Agent.DeepCopy()
		agent.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: "a"}, {Name: "b"}}
		agents.UpdateObject(agent)
		harness := harnesses.List()[0]
		harnesses.DeleteObject("team-a/byo")

		const tickets = "https://tickets.example.com:443"
		const controller = "https://kagent-controller.kagent:8083"
		policy := func(name, origin string) *v1alpha3.EgressPolicy {
			return &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: name},
				Spec: v1alpha3.EgressPolicySpec{Destinations: []v1alpha3.EgressDestination{{Origin: origin}}}}
		}
		policies.UpdateObject(policy("a", tickets))
		policies.UpdateObject(policy("b", controller))
		check := func(compiled, conflict bool, want ...string) {
			t.Helper()
			synctest.Wait()
			state := collections.Reconciliations.GetKey(initial.ResourceName())
			require.Equal(t, compiled, state.Target != nil)
			require.Equal(t, conflict, state.EgressConflict != "")
			require.NoError(t, r.reconcileAgent(t.Context(), initial.ResourceName()))
			require.ElementsMatch(t, want, store.pair.EgressGrant.Destinations)
		}
		// With no target, the requested grants are validated on their own.
		check(false, false, tickets, controller)
		// Restoring compilation introduces an HTTP base that conflicts with the
		// previously admitted HTTPS grant on the same host and port.
		harnesses.UpdateObject(harness)
		check(true, true, tickets)

		harnesses.DeleteObject("team-a/byo")
		conflicting := policy("b", "http://tickets.example.com:443")
		conflicting.Spec.Destinations = append(conflicting.Spec.Destinations, v1alpha3.EgressDestination{Origin: "https://new.example.com"})
		policies.UpdateObject(conflicting)
		// Conflicts between policies still hold additions without a target.
		check(false, true, tickets)
		// A replacement source cannot retain grants belonging to the old UID.
		agent = agent.DeepCopy()
		agent.UID = "replacement"
		agents.UpdateObject(agent)
		check(false, true)
	})
}
