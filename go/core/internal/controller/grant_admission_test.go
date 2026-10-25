package controller

import (
	"errors"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	atev1 "go.ate.dev/substrate/api/infra/k8s/ate/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kagentfake "github.com/kagent-dev/kagent/go/api/clientset/versioned/fake"
	v1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
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
					pools := krt.NewStaticCollection(nil, []*atev1.WorkerPool{{ObjectMeta: metav1.ObjectMeta{Namespace: template.Namespace, Name: "default"}}}, opts.WithName("pools")...)
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

func TestAgentGrantAdmissionReservesTokenPassthroughHeaders(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const origin = "https://mcp.example.com:443"
		server := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "user"}, Spec: v1alpha3.RemoteMCPServerSpec{URL: "https://mcp.example.com/mcp", Headers: []v1alpha3.Header{{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{TokenPassthrough: &v1alpha3.TokenPassthrough{}}}}}}
		template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "assistant", UID: "template-uid"}, Spec: v1alpha3.AgentTemplateSpec{
			Tools: []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{Server: corev1.TypedLocalObjectReference{Kind: "RemoteMCPServer", Name: server.Name}, Tools: []string{"lookup"}}}},
		}}
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "auth"}, Data: map[string][]byte{"token": []byte("value")}}
		collections, _ := newPreparationTestCollectionsFor(t, "gvisor", template, server, secret)
		initial := collections.Reconciliations.List()[0]
		target := egresspolicy.GatewayTarget{Origin: origin, Header: "authorization", Prefix: "Bearer "}
		require.Equal(t, []egresspolicy.GatewayTarget{target}, initial.Target.Revision.TokenPassthroughTargets)
		agent := initial.Agent.DeepCopy()
		agent.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: "keys"}}
		collections.Agents.(krt.StaticCollection[*v1alpha3.Agent]).UpdateObject(agent)
		prepared := proto.CloneOf(initial.Target.ActorTemplate)
		prepared.Metadata.Uid, prepared.Status = "prepared", sandboxGoldenStatus()
		store := &fakeRuntimeRevisionStore{}
		r := &Reconciler{collections: collections, templates: &fakeActorTemplates{template: prepared}, store: store}
		policies := collections.EgressPolicies.(krt.StaticCollection[*v1alpha3.EgressPolicy])
		// Only the same header conflicts; another credential can share the origin.
		for _, header := range []string{"Authorization", "X-API-Key"} {
			policies.UpdateObject(&v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "keys"}, Spec: v1alpha3.EgressPolicySpec{Destinations: []v1alpha3.EgressDestination{{
				Origin: origin, Headers: []v1alpha3.Header{{Name: header, ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: new(corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "auth"}, Key: "token"})}}},
			}}}})
			synctest.Wait()
			conflict := collections.Reconciliations.GetKey(initial.ResourceName()).EgressConflict
			require.NoError(t, r.reconcileAgent(t.Context(), initial.ResourceName()))
			if header == "Authorization" {
				require.NotEmpty(t, conflict)
				require.Empty(t, store.pair.EgressGrant.Credentials)
			} else {
				require.Empty(t, conflict)
				require.Len(t, store.pair.EgressGrant.Credentials, 1)
			}
		}
		require.Equal(t, []egresspolicy.GatewayTarget{target}, store.revision.TokenPassthroughTargets)
	})
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

func TestIntegrationAndPolicyPassthroughShareGrantLifetime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		header := v1alpha3.Header{Name: "Authorization", Prefix: new("Bearer "), ValueFrom: v1alpha3.HeaderValueSource{TokenPassthrough: &v1alpha3.TokenPassthrough{}}}
		server := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "mcp", UID: "mcp-uid"}, Spec: v1alpha3.RemoteMCPServerSpec{URL: "https://mcp.example.com/mcp", Headers: []v1alpha3.Header{header}}}
		template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "assistant", UID: "template-uid"}, Spec: v1alpha3.AgentTemplateSpec{
			Tools: []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{Server: corev1.TypedLocalObjectReference{Kind: "RemoteMCPServer", Name: server.Name}, Tools: []string{"lookup"}}}},
		}}
		collections, harnesses := newPreparationTestCollectionsFor(t, "gvisor", template, server)
		initial := collections.Reconciliations.List()[0]
		base := egress.Access{Destinations: initial.Target.Revision.EgressDestinations, TokenPassthroughTargets: initial.Target.Revision.TokenPassthroughTargets}
		prepared := proto.CloneOf(initial.Target.ActorTemplate)
		prepared.Metadata.Uid, prepared.Status = "prepared", sandboxGoldenStatus()
		store := &fakeRuntimeRevisionStore{}
		r := &Reconciler{collections: collections, templates: &fakeActorTemplates{template: prepared}, store: store}
		check := func(allowed bool) {
			t.Helper()
			synctest.Wait()
			require.NoError(t, r.reconcileAgent(t.Context(), initial.ResourceName()))
			access, err := egress.EffectiveAccess(base, store.pair.EgressGrant)
			require.NoError(t, err)
			if allowed {
				require.Equal(t, base.TokenPassthroughTargets, access.TokenPassthroughTargets)
			} else {
				require.Empty(t, access.TokenPassthroughTargets)
			}
		}
		check(true)
		agent := initial.Agent.DeepCopy()
		agent.Spec.Template = v1alpha3.AgentTemplateSource{Inline: &v1alpha3.AgentTemplateSpec{}}
		collections.Agents.(krt.StaticCollection[*v1alpha3.Agent]).UpdateObject(agent)
		check(false)
		policy := &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "access"}, Spec: v1alpha3.EgressPolicySpec{Destinations: []v1alpha3.EgressDestination{{Origin: "https://mcp.example.com", Headers: []v1alpha3.Header{header}}}}}
		policies := collections.EgressPolicies.(krt.StaticCollection[*v1alpha3.EgressPolicy])
		policies.UpdateObject(policy)
		agent = agent.DeepCopy()
		agent.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: policy.Name}}
		collections.Agents.(krt.StaticCollection[*v1alpha3.Agent]).UpdateObject(agent)
		check(true)
		harness := harnesses.List()[0]
		harnesses.DeleteObject("team-a/byo")
		check(true)
		policies.DeleteObject("team-a/access")
		check(false)
		// Failed runtime compilation preserves the applied integration while
		// continuing to reconcile policy and integration source revocations.
		collections.Agents.(krt.StaticCollection[*v1alpha3.Agent]).UpdateObject(initial.Agent.DeepCopy())
		harnesses.UpdateObject(harness)
		check(true)
		harnesses.DeleteObject("team-a/byo")
		check(true)
		uncompiled := initial.Agent.DeepCopy()
		uncompiled.Spec.Template = v1alpha3.AgentTemplateSource{Inline: &v1alpha3.AgentTemplateSpec{}}
		collections.Agents.(krt.StaticCollection[*v1alpha3.Agent]).UpdateObject(uncompiled)
		check(true)
		collections.Agents.(krt.StaticCollection[*v1alpha3.Agent]).UpdateObject(initial.Agent.DeepCopy())
		check(true)
		servers := collections.RemoteMCPServers.(krt.StaticCollection[*v1alpha3.RemoteMCPServer])
		invalid := server.DeepCopy()
		invalid.Spec.Headers[0].Prefix = new("invalid\r\n")
		servers.UpdateObject(invalid)
		check(true)
		removed := server.DeepCopy()
		removed.Spec.Headers = nil
		servers.UpdateObject(removed)
		check(false)
		servers.UpdateObject(invalid)
		check(false)
		// A separate policy may still grant the same header. Neither that
		// overlap nor restart can erase the integration source's revocation.
		policies.UpdateObject(policy)
		agent = initial.Agent.DeepCopy()
		agent.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: policy.Name}}
		collections.Agents.(krt.StaticCollection[*v1alpha3.Agent]).UpdateObject(agent)
		check(true)
		collections.AgentRuntimeObservations.DeleteObject(initial.ResourceName())
		synctest.Wait()
		require.NoError(t, r.reconcileAgent(t.Context(), initial.ResourceName()))
		check(true)
		policies.DeleteObject("team-a/access")
		check(false)
		servers.UpdateObject(server)
		check(true)
		for _, readUnavailable := range []bool{false, true} {
			// The fake store commits the definition before returning pairErr.
			// Recovery must prefer that durable revocation over the older cache.
			store.pairErr = errors.New("commit response lost")
			if readUnavailable {
				store.integrationReadErr = errors.New("database unavailable")
			}
			servers.UpdateObject(removed)
			synctest.Wait()
			require.Error(t, r.reconcileAgent(t.Context(), initial.ResourceName()))
			require.Empty(t, store.pair.EgressGrant.TokenPassthroughTargets)
			store.pairErr, store.integrationReadErr = nil, nil
			servers.UpdateObject(invalid)
			synctest.Wait()
			if readUnavailable {
				require.NoError(t, r.reconcileAgent(t.Context(), initial.ResourceName()))
			}
			check(false)
			servers.UpdateObject(server)
			check(true)
		}
		servers.DeleteObject("team-a/mcp")
		check(false)
		replacement := server.DeepCopy()
		replacement.UID = "replacement"
		servers.UpdateObject(replacement)
		check(false)
		harnesses.UpdateObject(harness)
		check(true)
	})
}

func TestAppliedIntegrationGrantsRecoverBeforeRuntimePreparation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		header := v1alpha3.Header{Name: "Authorization", ValueFrom: v1alpha3.HeaderValueSource{TokenPassthrough: &v1alpha3.TokenPassthrough{}}}
		server := &v1alpha3.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "mcp", UID: "mcp-uid"}, Spec: v1alpha3.RemoteMCPServerSpec{URL: "https://mcp.example.com/mcp", Headers: []v1alpha3.Header{header}}}
		template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "assistant", UID: "template-uid"}, Spec: v1alpha3.AgentTemplateSpec{
			Tools: []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{Server: corev1.TypedLocalObjectReference{Kind: "RemoteMCPServer", Name: server.Name}, Tools: []string{"lookup"}}}},
		}}
		collections, harnesses := newPreparationTestCollectionsFor(t, "gvisor", template, server)
		initial := collections.Reconciliations.List()[0]
		store := &fakeRuntimeRevisionStore{}
		backend := &fakeActorTemplates{getErr: status.Error(codes.Unavailable, "unavailable")}
		r := &Reconciler{collections: collections, templates: backend, store: store}
		require.Error(t, r.reconcileAgent(t.Context(), initial.ResourceName()))
		require.Nil(t, store.revision, "grants can be applied before any revision snapshot is stored")
		require.Len(t, store.pair.IntegrationGrants, 1)
		applied := store.pair.EgressGrant

		// Lose all observations as on restart, with unresolvable runtime inputs.
		harnesses.DeleteObject("team-a/byo")
		collections.AgentRuntimeObservations.DeleteObject(initial.ResourceName())
		synctest.Wait()
		require.NoError(t, r.reconcileAgent(t.Context(), initial.ResourceName()))
		require.Equal(t, applied, store.pair.EgressGrant, "hydration must not transiently replace applied grants")
		synctest.Wait()
		require.NoError(t, r.reconcileAgent(t.Context(), initial.ResourceName()))
		require.Equal(t, applied.TokenPassthroughTargets, store.pair.EgressGrant.TokenPassthroughTargets)

		// Policy updates and removals still apply while the old integration is retained.
		policy := &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "access"}, Spec: v1alpha3.EgressPolicySpec{
			Destinations: []v1alpha3.EgressDestination{{Origin: "https://tickets.example.com", Headers: []v1alpha3.Header{header}}},
		}}
		policies := collections.EgressPolicies.(krt.StaticCollection[*v1alpha3.EgressPolicy])
		policies.UpdateObject(policy)
		agent := initial.Agent.DeepCopy()
		agent.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: policy.Name}}
		collections.Agents.(krt.StaticCollection[*v1alpha3.Agent]).UpdateObject(agent)
		synctest.Wait()
		require.NoError(t, r.reconcileAgent(t.Context(), initial.ResourceName()))
		require.Len(t, store.pair.EgressGrant.TokenPassthroughTargets, 2)
		policies.DeleteObject("team-a/access")
		synctest.Wait()
		require.NoError(t, r.reconcileAgent(t.Context(), initial.ResourceName()))
		require.Equal(t, applied.TokenPassthroughTargets, store.pair.EgressGrant.TokenPassthroughTargets)

		// Replacing the Agent itself must not inherit its old integration bindings.
		agent = agent.DeepCopy()
		agent.UID = "replacement-agent"
		collections.Agents.(krt.StaticCollection[*v1alpha3.Agent]).UpdateObject(agent)
		synctest.Wait()
		require.NoError(t, r.reconcileAgent(t.Context(), initial.ResourceName()))
		require.Empty(t, store.pair.EgressGrant.TokenPassthroughTargets)
		require.Empty(t, store.pair.IntegrationGrants)
	})
}
