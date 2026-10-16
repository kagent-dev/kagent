package controller

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kagentfake "github.com/kagent-dev/kagent/go/api/clientset/versioned/fake"
	v1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

func TestAgentGrantChangesReconcileWithoutRuntimeChange(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		collections, harnesses := newPreparationTestCollections(t, "gvisor")
		initial := collections.Reconciliations.List()[0]
		template := proto.CloneOf(initial.Target.ActorTemplate)
		template.Metadata.Uid = "prepared"
		template.Status = sandboxGoldenStatus()
		store := &fakeRuntimeRevisionStore{}
		backend := &preparationTestBackend{templates: &fakeActorTemplates{template: template}, store: store}
		var sources []database.RuntimeEgressSource
		r := newReconciler(collections, backend, backend, kagentfake.NewSimpleClientset(initial.Agent.DeepCopy()).ApiV1alpha3(), func(source database.RuntimeEgressSource) { sources = append(sources, source) })
		ctx, cancel := context.WithCancel(t.Context())
		var workers sync.WaitGroup
		workers.Go(func() { r.Run(ctx.Done()) })
		defer func() { cancel(); workers.Wait() }()
		synctest.Wait()
		sources = nil
		policy := &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "access"}, Spec: v1alpha3.EgressPolicySpec{Destinations: []v1alpha3.EgressDestination{{Origin: "https://tickets.example.com"}}}}
		collections.EgressPolicies.(krt.StaticCollection[*v1alpha3.EgressPolicy]).UpdateObject(policy)
		agent := initial.Agent.DeepCopy()
		agent.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: policy.Name}}
		collections.Agents.(krt.StaticCollection[*v1alpha3.Agent]).UpdateObject(agent)
		synctest.Wait()
		backend.inspect(func() {
			require.Equal(t, initial.Target.RevisionID.String(), store.pair.DesiredRevision)
			require.Equal(t, []string{"https://tickets.example.com:443"}, store.pair.Grants.Destinations)
		})
		require.Equal(t, []database.RuntimeEgressSource{{Kind: database.RuntimeEgressAgent, Namespace: "team-a", Name: "assistant"}}, sources)
		sources = nil
		harnesses.DeleteObject("team-a/byo")
		synctest.Wait()
		backend.inspect(func() {
			require.Empty(t, store.pair.DesiredRevision)
			require.NotEmpty(t, store.pair.Grants.Destinations)
		})
		require.Empty(t, sources, "unrelated runtime failure must not fan out unchanged grants")
		collections.EgressPolicies.(krt.StaticCollection[*v1alpha3.EgressPolicy]).DeleteObject("team-a/access")
		synctest.Wait()
		backend.inspect(func() { require.Empty(t, store.pair.DesiredRevision); require.Empty(t, store.pair.Grants.Destinations) })
		require.Len(t, sources, 1, "revocation must reconcile even while the runtime remains unresolvable")
	})
}

func TestAgentGrantsSurviveUnrelatedCompilationFailure(t *testing.T) {
	for _, missing := range []string{"Harness", "AgentTemplate", "WorkerPool"} {
		t.Run(missing, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				collections, harnesses := newPreparationTestCollections(t, "gvisor")
				agent := collections.Agents.List()[0].DeepCopy()
				agent.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: "access"}}
				switch missing {
				case "Harness":
					agent.Spec.HarnessRef.Name = "missing"
				case "AgentTemplate":
					agent.Spec.Template = v1alpha3.AgentTemplateSource{Ref: &corev1.LocalObjectReference{Name: "missing"}}
				case "WorkerPool":
					harness := harnesses.List()[0].DeepCopy()
					harness.Spec.Substrate.WorkerPoolRef.Name = "missing"
					harnesses.UpdateObject(harness)
				}
				collections.Agents.(krt.StaticCollection[*v1alpha3.Agent]).UpdateObject(agent)
				synctest.Wait()
				store := &fakeRuntimeRevisionStore{}
				backend := &preparationTestBackend{templates: &fakeActorTemplates{}, store: store}
				var sources []database.RuntimeEgressSource
				r := newReconciler(collections, backend, backend, kagentfake.NewSimpleClientset(agent.DeepCopy()).ApiV1alpha3(), func(source database.RuntimeEgressSource) { sources = append(sources, source) })
				ctx, cancel := context.WithCancel(t.Context())
				var workers sync.WaitGroup
				workers.Go(func() { r.Run(ctx.Done()) })
				defer func() { cancel(); workers.Wait() }()
				synctest.Wait()
				sources = nil

				policies := collections.EgressPolicies.(krt.StaticCollection[*v1alpha3.EgressPolicy])
				policy := &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "access"}, Spec: v1alpha3.EgressPolicySpec{Destinations: []v1alpha3.EgressDestination{{Origin: "https://tickets.example.com"}}}}
				policies.UpdateObject(policy)
				synctest.Wait()
				state := collections.Reconciliations.GetKey("team-a/assistant")
				require.Nil(t, state.Target)
				require.NotNil(t, state.CompilationFailure)
				require.Contains(t, state.CompilationFailure.Message, missing)
				backend.inspect(func() {
					require.Empty(t, store.pair.DesiredRevision)
					require.Equal(t, []string{"https://tickets.example.com:443"}, store.pair.Grants.Destinations)
				})
				wantSource := database.RuntimeEgressSource{Kind: database.RuntimeEgressAgent, Namespace: "team-a", Name: "assistant"}
				require.Equal(t, []database.RuntimeEgressSource{wantSource}, sources)

				policy = policy.DeepCopy()
				policy.Spec.Destinations[0].Origin = "https://updated.example.com"
				policies.UpdateObject(policy)
				synctest.Wait()
				backend.inspect(func() {
					require.Empty(t, store.pair.DesiredRevision)
					require.Equal(t, []string{"https://updated.example.com:443"}, store.pair.Grants.Destinations)
				})
				require.Equal(t, []database.RuntimeEgressSource{wantSource, wantSource}, sources)

				policies.DeleteObject("team-a/access")
				synctest.Wait()
				backend.inspect(func() {
					require.Empty(t, store.pair.DesiredRevision)
					require.Empty(t, store.pair.Grants.Destinations)
					require.Zero(t, store.recordCalls, "grant changes must not require a compiled runtime")
				})
				require.Equal(t, []database.RuntimeEgressSource{wantSource, wantSource, wantSource}, sources, "revocation must enqueue actors while compilation remains broken")
			})
		})
	}
}

func TestAgentGrantWriteFailureAndRetirementEnqueueActors(t *testing.T) {
	collections, _ := newPreparationTestCollections(t, "gvisor")
	state := collections.Reconciliations.List()[0]
	store := &fakeRuntimeRevisionStore{pairErr: errors.New("response lost")}
	var sources []database.RuntimeEgressSource
	r := &Reconciler{collections: collections, store: store, enqueueActors: func(source database.RuntimeEgressSource) { sources = append(sources, source) }, status: kagentfake.NewSimpleClientset(state.Agent.DeepCopy()).ApiV1alpha3()}
	require.ErrorIs(t, r.reconcileAgent(t.Context(), state.ResourceName()), store.pairErr)
	require.Len(t, sources, 1, "a failed response can hide a committed grants update")
	collections.Agents.(krt.StaticCollection[*v1alpha3.Agent]).DeleteObject(state.ResourceName())
	waitFor(t, func() bool { return collections.Reconciliations.GetKey(state.ResourceName()) == nil })
	require.NoError(t, r.reconcileAgent(t.Context(), state.ResourceName()))
	require.Len(t, sources, 2)
	require.Equal(t, sources[0], sources[1])
}

func TestAgentFinalizerRetiresGrantsAfterOfflineDeletion(t *testing.T) {
	collections, _ := newPreparationTestCollections(t, "gvisor")
	agent := collections.Agents.List()[0].DeepCopy()
	agent.Finalizers = nil
	agents := collections.Agents.(krt.StaticCollection[*v1alpha3.Agent])
	agents.UpdateObject(agent)
	waitFor(t, func() bool { return len(collections.Reconciliations.GetKey("team-a/assistant").Agent.Finalizers) == 0 })
	client := kagentfake.NewSimpleClientset(agent.DeepCopy()).ApiV1alpha3()
	store := &fakeRuntimeRevisionStore{}
	var sources []database.RuntimeEgressSource
	r := &Reconciler{collections: collections, store: store, status: client, enqueueActors: func(source database.RuntimeEgressSource) { sources = append(sources, source) }}
	require.NoError(t, r.reconcileAgent(t.Context(), "team-a/assistant"))
	require.Zero(t, store.pairCalls, "deletion ownership must be durable before grants")
	updated, err := client.Agents("team-a").Get(t.Context(), "assistant", metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, updated.Finalizers, agentPreparationFinalizer)
	updated.DeletionTimestamp = &metav1.Time{Time: metav1.Now().Time}
	agents.UpdateObject(updated)
	waitFor(t, func() bool {
		return !collections.Reconciliations.GetKey("team-a/assistant").Agent.DeletionTimestamp.IsZero()
	})
	require.NoError(t, r.reconcileAgent(t.Context(), "team-a/assistant"))
	require.NotEmpty(t, store.retired)
	require.Len(t, sources, 1)
	persisted, err := client.Agents("team-a").Get(t.Context(), "assistant", metav1.GetOptions{})
	require.NoError(t, err)
	require.NotContains(t, persisted.Finalizers, agentPreparationFinalizer)
}

func TestSandboxGrantsSurviveUnrelatedCompilationFailure(t *testing.T) {
	opts := krt.NewOptionsBuilder(t.Context().Done(), "test-grants", nil)
	template := sandboxTestTemplate()
	template.Finalizers = []string{sandboxPreparationFinalizer}
	template.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: "access"}}
	templates := krt.NewStaticCollection(nil, []*v1alpha3.SandboxTemplate{template}, opts.WithName("Templates")...)
	policy := &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "access"}, Spec: v1alpha3.EgressPolicySpec{Destinations: []v1alpha3.EgressDestination{{Origin: "https://tickets.example.com"}}}}
	policies := krt.NewStaticCollection(nil, []*v1alpha3.EgressPolicy{policy}, opts.WithName("Policies")...)
	pools := krt.NewStaticCollection[*atev1alpha1.WorkerPool](nil, nil, opts.WithName("Pools")...)
	store := &sandboxTestStore{}
	var sources []database.RuntimeEgressSource
	s := &SandboxReconciler{collections: newSandboxCollections(Collections{SandboxTemplates: templates, WorkerPools: pools, EgressPolicies: policies}, substrate.SandboxPolicy{}, opts), store: store, client: kagentfake.NewSimpleClientset(template.DeepCopy()).ApiV1alpha3(), enqueueActors: func(source database.RuntimeEgressSource) { sources = append(sources, source) }}
	waitFor(t, func() bool { return s.collections.states.GetKey("team-a/scratch") != nil })
	require.NoError(t, s.reconcile(t.Context(), "team-a/scratch"))
	require.Nil(t, s.collections.states.GetKey("team-a/scratch").DesiredActorTemplate, "missing WorkerPool prevents runtime compilation")
	require.Equal(t, []string{"https://tickets.example.com:443"}, store.desired.Grants.Destinations)
	require.Len(t, sources, 1)
	policies.DeleteObject("team-a/access")
	waitFor(t, func() bool { return s.collections.states.GetKey("team-a/scratch").CompilationError != "" })
	require.Error(t, s.reconcile(t.Context(), "team-a/scratch"))
	require.Empty(t, store.desired.Grants.Destinations)
	require.Len(t, sources, 2, "revocation must reach existing actors during failed compilation")
	store.upsertErr = errors.New("response lost")
	require.ErrorIs(t, s.reconcile(t.Context(), "team-a/scratch"), store.upsertErr)
	require.Len(t, sources, 3)
	template = template.DeepCopy()
	template.DeletionTimestamp = &metav1.Time{Time: metav1.Now().Time}
	templates.UpdateObject(template)
	waitFor(t, func() bool { return !s.collections.states.GetKey("team-a/scratch").Template.DeletionTimestamp.IsZero() })
	require.NoError(t, s.reconcile(t.Context(), "team-a/scratch"))
	require.Len(t, sources, 4)
}
