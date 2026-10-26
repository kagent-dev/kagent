package controller

import (
	"encoding/json"
	"testing"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

// Recovery starts with only the edited spec in Kubernetes. The old reference
// must be restored from PostgreSQL and remain reactive until fallback is revoked.
func TestPreparedReferenceRevocationAfterEditAndRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	for _, kind := range []string{"ModelConfig", "RemoteMCPServer"} {
		for _, selector := range []bool{false, true} {
			name := kind + "/policy"
			if selector {
				name = kind + "/namespace-labels"
			}
			t.Run(name, func(t *testing.T) {
				ctx := t.Context()
				dsn := dbtest.StartT(ctx, t)
				dbtest.MigrateT(t, dsn, false)
				pool, err := database.Connect(ctx, &database.PostgresConfig{Pool: dbtest.PoolConfig(dsn)})
				require.NoError(t, err)
				t.Cleanup(pool.Close)
				store := database.NewClient(pool, "public")
				opts := krt.NewOptionsBuilder(ctx.Done(), "prepared-policy", nil)
				policy := &v1alpha3.AllowedNamespaces{From: v1alpha3.NamespacesFromAll}
				if selector {
					policy = &v1alpha3.AllowedNamespaces{From: v1alpha3.NamespacesFromSelector, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "engineering"}}}
				}
				model := &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "shared", Name: "old"}, Spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Model: "example", AllowedNamespaces: policy}}
				server := &v1alpha3.RemoteMCPServer{ObjectMeta: model.ObjectMeta, Spec: v1alpha3.RemoteMCPServerSpec{URL: "https://tools.example.com", AllowedNamespaces: policy}}
				models := krt.NewStaticCollection(nil, []*v1alpha3.ModelConfig{model}, opts.WithName("Models")...)
				servers := krt.NewStaticCollection(nil, []*v1alpha3.RemoteMCPServer{server}, opts.WithName("Servers")...)
				runner := harness("team-a", "runner", nil)
				runner.Spec.Kagent = &v1alpha3.KagentHarness{}
				agent := &v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "agent", UID: "agent-uid"}, Spec: v1alpha3.AgentSpec{
					HarnessRef: corev1.LocalObjectReference{Name: runner.Name},
					Template:   v1alpha3.AgentTemplateSource{Inline: &v1alpha3.AgentTemplateSpec{ModelConfig: &v1alpha3.ObjectReference{Name: "missing-replacement"}}},
				}}
				agents := krt.NewStaticCollection(nil, []*v1alpha3.Agent{agent}, opts.WithName("Agents")...)
				prepared := krt.NewStaticCollection[translator.PreparedAgentReferences](nil, nil, opts.WithName("Prepared")...)
				namespaces := krt.NewStaticCollection(nil, []translator.NamespaceObservation{{Name: "team-a", Namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team-a", Labels: map[string]string{"team": "engineering"}}}}}, opts.WithName("Namespaces")...)
				mock := krttest.NewMock(t, []any{runner})
				inputs := translator.Collections{
					PreparedReferences: prepared, Namespaces: namespaces,
					Harnesses: krttest.GetMockCollection[*v1alpha3.Harness](mock), AgentTemplates: krttest.GetMockCollection[*v1alpha3.AgentTemplate](mock),
					RemoteMCPServers: servers, Secrets: krttest.GetMockCollection[*corev1.Secret](mock), ConfigMaps: krttest.GetMockCollection[*corev1.ConfigMap](mock),
				}
				_, inputs.ResolvedModelConfigs = newModelConfigReconciliations(models, inputs.ConfigMaps, inputs.Secrets, opts)
				observations := krt.NewStaticCollection[AgentRuntimeObservation](nil, nil, opts.WithName("Observations")...)
				states := newAgentReconciliations(agents, inputs, observations, opts, testRuntimeDefaults(), nil)
				reconciler := &Reconciler{store: store, templates: &fakeActorTemplates{}, collections: Collections{PreparedReferences: &prepared, Reconciliations: states, AgentRuntimeObservations: observations}}
				snapshot, err := json.Marshal(map[string]any{"inputs": []map[string]string{{"apiVersion": v1alpha3.GroupVersion.String(), "kind": kind, "name": "shared/old"}}})
				require.NoError(t, err)
				require.NoError(t, store.UpsertAgentDefinition(ctx, database.AgentDefinition{Namespace: agent.Namespace, AgentName: agent.Name, AgentUID: string(agent.UID), DesiredRevision: "r1"}))
				require.NoError(t, store.RecordRuntimeRevision(ctx, database.RuntimeRevision{Revision: "r1", Namespace: agent.Namespace, AgentName: agent.Name, AgentUID: string(agent.UID), SourceSnapshot: snapshot, AgentCard: &a2apb.AgentCard{Name: agent.Name}, ActorTemplateAtespace: agent.Namespace, ActorTemplateName: "r1", ActorTemplateUID: "actor-uid"}, database.RecordRevisionOptions{Ready: true}))
				create := func(key string) (*apiv1.Session, bool, error) {
					return store.CreateSession(ctx, &apiv1.Session{Id: uuid.NewString(), Creator: "alice", Agent: &apiv1.ResourceReference{Namespace: agent.Namespace, Name: agent.Name}}, key)
				}
				pinned, _, err := create("pinned")
				require.NoError(t, err)
				key := agent.Namespace + "/" + agent.Name
				waitFor(t, func() bool { return states.GetKey(key) != nil })
				require.NoError(t, reconciler.reconcileAgent(ctx, key))
				waitFor(t, func() bool {
					s := states.GetKey(key)
					return s.PreparedReferences != nil && s.PreparedReferences.Revision == "r1"
				})
				require.False(t, states.GetKey(key).revokesPreparedRevision())
				require.NoError(t, reconciler.reconcileAgent(ctx, key))
				fallback, _, err := create("fallback")
				require.NoError(t, err)
				require.Equal(t, "r1", fallback.PreparedRevision)
				require.Equal(t, selector, translator.AgentNeedsNamespace(krt.TestingDummyContext{}, inputs, agent))
				if selector {
					namespaces.UpdateObject(translator.NamespaceObservation{Name: "team-a", Namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team-a"}}})
				} else if kind == "ModelConfig" {
					updated := model.DeepCopy()
					updated.Spec.AllowedNamespaces.From = v1alpha3.NamespacesFromSame
					models.UpdateObject(updated)
				} else {
					updated := server.DeepCopy()
					updated.Spec.AllowedNamespaces.From = v1alpha3.NamespacesFromSame
					servers.UpdateObject(updated)
				}
				waitFor(t, func() bool { return states.GetKey(key).revokesPreparedRevision() })
				require.NoError(t, reconciler.reconcileAgent(ctx, key))
				_, _, err = create("denied")
				require.ErrorIs(t, err, database.ErrNotFound)
				existing, err := store.GetSessionByID(ctx, pinned.Id)
				require.NoError(t, err)
				require.Equal(t, "r1", existing.PreparedRevision)
				waitFor(t, func() bool { return states.GetKey(key).PreparedReferences.Revision == "" })
				require.False(t, translator.AgentNeedsNamespace(krt.TestingDummyContext{}, inputs, agent))
			})
		}
	}
}

func TestPreparedReferenceReplacementStopsOldPolicyTracking(t *testing.T) {
	opts := krt.NewOptionsBuilder(t.Context().Done(), "replacement", nil)
	old := &v1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "shared", Name: "old"}, Spec: v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Model: "example", AllowedNamespaces: &v1alpha3.AllowedNamespaces{From: v1alpha3.NamespacesFromSame}}}
	replacement := old.DeepCopy()
	replacement.Name = "new"
	replacement.Spec.AllowedNamespaces.From = v1alpha3.NamespacesFromAll
	mock := krttest.NewMock(t, []any{})
	inputs := translator.Collections{
		Harnesses: krttest.GetMockCollection[*v1alpha3.Harness](mock), AgentTemplates: krttest.GetMockCollection[*v1alpha3.AgentTemplate](mock), RemoteMCPServers: krttest.GetMockCollection[*v1alpha3.RemoteMCPServer](mock),
		ResolvedModelConfigs: krt.NewStaticCollection(nil, []translator.ResolvedModelConfig{{Config: old}, {Config: replacement}}, opts.WithName("Models")...),
	}
	agent := &v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "agent", UID: "uid"}, Spec: v1alpha3.AgentSpec{HarnessRef: corev1.LocalObjectReference{Name: "missing"}, Template: v1alpha3.AgentTemplateSource{Inline: &v1alpha3.AgentTemplateSpec{}}}}
	cached := translator.PreparedAgentReferences{Namespace: agent.Namespace, AgentName: agent.Name, AgentUID: string(agent.UID), Revision: "r1", References: []translator.PreparedReference{{Kind: "ModelConfig", Name: types.NamespacedName{Namespace: "shared", Name: "old"}}}}
	prepared := krt.NewStaticCollection(nil, []translator.PreparedAgentReferences{cached}, opts.WithName("Prepared")...)
	inputs.PreparedReferences = prepared
	agents := krt.NewStaticCollection(nil, []*v1alpha3.Agent{agent}, opts.WithName("Agents")...)
	states := newAgentReconciliations(agents, inputs, krttest.GetMockCollection[AgentRuntimeObservation](mock), opts, testRuntimeDefaults(), nil)
	key := agent.Namespace + "/" + agent.Name
	waitFor(t, func() bool { return states.GetKey(key) != nil && states.GetKey(key).revokesPreparedRevision() })
	cached.Revision = "r2"
	cached.References = []translator.PreparedReference{{Kind: "ModelConfig", Name: types.NamespacedName{Namespace: "shared", Name: "new"}}}
	prepared.UpdateObject(cached)
	waitFor(t, func() bool {
		s := states.GetKey(key)
		return s.PreparedReferences.Revision == "r2" && !s.revokesPreparedRevision()
	})
	require.Nil(t, states.GetKey(key).PreparedPolicyFailure, "revoked old reference must not deny the replacement revision")
}
