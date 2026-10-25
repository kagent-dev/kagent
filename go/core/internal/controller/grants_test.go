package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	atev1 "go.ate.dev/substrate/api/infra/k8s/ate/v1"
	"google.golang.org/protobuf/proto"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kagentfake "github.com/kagent-dev/kagent/go/api/clientset/versioned/fake"
	v1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

func TestAgentEgressErrorsDoNotBlockCompilation(t *testing.T) {
	for _, test := range []struct {
		name         string
		destinations []v1alpha3.EgressDestination
		condition    string
		reason       string
	}{
		{name: "missing policy", condition: v1alpha3.AgentConditionEgressPoliciesApplied, reason: v1alpha3.EgressPoliciesReasonNotFound},
		{name: "invalid origin", destinations: []v1alpha3.EgressDestination{{Origin: "invalid"}}, condition: v1alpha3.AgentConditionEgressPoliciesApplied, reason: v1alpha3.EgressPoliciesReasonInvalidEgressPolicy},
		{name: "missing secret", destinations: []v1alpha3.EgressDestination{{Origin: "https://tickets.example.com", Headers: []v1alpha3.Header{{Name: "Authorization", ValueFrom: v1alpha3.HeaderValueSource{SecretKeyRef: new(corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "missing"}, Key: "token"})}}}}}, condition: v1alpha3.AgentConditionEgressPoliciesApplied, reason: v1alpha3.EgressPoliciesReasonInvalidEgressPolicy},
	} {
		t.Run(test.name, func(t *testing.T) {
			collections, _ := newPreparationTestCollections(t, "gvisor")
			if test.destinations != nil {
				collections.EgressPolicies.(krt.StaticCollection[*v1alpha3.EgressPolicy]).UpdateObject(&v1alpha3.EgressPolicy{
					ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "access"},
					Spec:       v1alpha3.EgressPolicySpec{Destinations: test.destinations},
				})
			}
			agent := collections.Agents.List()[0].DeepCopy()
			agent.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: "access"}}
			collections.Agents.(krt.StaticCollection[*v1alpha3.Agent]).UpdateObject(agent)
			waitFor(t, func() bool {
				observed := collections.AgentStatuses.GetKey("team-a/assistant")
				return observed != nil && meta.IsStatusConditionFalse(observed.Status.Conditions, test.condition)
			})
			condition := meta.FindStatusCondition(collections.AgentStatuses.GetKey("team-a/assistant").Status.Conditions, test.condition)
			require.Equal(t, test.reason, condition.Reason)
			state := collections.Reconciliations.GetKey("team-a/assistant")
			require.NotNil(t, state.Target)
			require.Nil(t, state.CompilationFailure)
			require.Empty(t, state.EgressGrant.Destinations)
			require.Empty(t, state.EgressGrant.Credentials)
		})
	}
}

func TestAgentGrantChangesReconcileWithoutRuntimeChange(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		collections, harnesses := newPreparationTestCollections(t, "gvisor")
		initial := collections.Reconciliations.List()[0]
		template := proto.CloneOf(initial.Target.ActorTemplate)
		template.Metadata.Uid = "prepared"
		template.Status = sandboxGoldenStatus()
		store := &fakeRuntimeRevisionStore{definitionSources: []database.RuntimeEgressSource{{Kind: database.RuntimeEgressAgent, Namespace: "team-a", Name: "assistant"}}}
		backend := &preparationTestBackend{templates: &fakeActorTemplates{template: template}, store: store}
		r := newReconciler(collections, backend, backend, kagentfake.NewSimpleClientset(initial.Agent.DeepCopy()).ApiV1alpha3())
		ctx, cancel := context.WithCancel(t.Context())
		var workers sync.WaitGroup
		workers.Go(func() { assert.NoError(t, r.Start(ctx)) })
		defer func() { cancel(); workers.Wait() }()
		synctest.Wait()
		require.Empty(t, store.retired, "startup must preserve a matching live Agent")
		store.sources = nil
		policy := &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "access"}, Spec: v1alpha3.EgressPolicySpec{Destinations: []v1alpha3.EgressDestination{{Origin: "https://tickets.example.com"}}}}
		collections.EgressPolicies.(krt.StaticCollection[*v1alpha3.EgressPolicy]).UpdateObject(policy)
		agent := initial.Agent.DeepCopy()
		agent.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: policy.Name}}
		collections.Agents.(krt.StaticCollection[*v1alpha3.Agent]).UpdateObject(agent)
		synctest.Wait()
		backend.inspect(func() {
			require.Equal(t, initial.Target.RevisionID.String(), store.pair.DesiredRevision)
			require.Equal(t, []string{"https://tickets.example.com:443"}, store.pair.EgressGrant.Destinations)
		})
		require.Equal(t, []database.RuntimeEgressSource{{Kind: database.RuntimeEgressAgent, Namespace: "team-a", Name: "assistant"}}, store.sources)
		store.sources = nil
		harnesses.DeleteObject("team-a/byo")
		synctest.Wait()
		backend.inspect(func() {
			require.Empty(t, store.pair.DesiredRevision)
			require.NotEmpty(t, store.pair.EgressGrant.Destinations)
		})
		require.Len(t, store.sources, 1, "every preparation reapplies grants, including unchanged grants")
		store.sources = nil
		collections.EgressPolicies.(krt.StaticCollection[*v1alpha3.EgressPolicy]).DeleteObject("team-a/access")
		synctest.Wait()
		backend.inspect(func() {
			require.Empty(t, store.pair.DesiredRevision)
			require.Empty(t, store.pair.EgressGrant.Destinations)
		})
		require.Len(t, store.sources, 1, "revocation must reconcile even while the runtime remains unresolvable")
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
				r := newReconciler(collections, backend, backend, kagentfake.NewSimpleClientset(agent.DeepCopy()).ApiV1alpha3())
				ctx, cancel := context.WithCancel(t.Context())
				var workers sync.WaitGroup
				workers.Go(func() { assert.NoError(t, r.Start(ctx)) })
				defer func() { cancel(); workers.Wait() }()
				synctest.Wait()
				store.sources = nil

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
					require.Equal(t, []string{"https://tickets.example.com:443"}, store.pair.EgressGrant.Destinations)
				})
				wantSource := database.RuntimeEgressSource{Kind: database.RuntimeEgressAgent, Namespace: "team-a", Name: "assistant"}
				require.Equal(t, []database.RuntimeEgressSource{wantSource}, store.sources)

				policy = policy.DeepCopy()
				policy.Spec.Destinations[0].Origin = "https://updated.example.com"
				policies.UpdateObject(policy)
				synctest.Wait()
				backend.inspect(func() {
					require.Empty(t, store.pair.DesiredRevision)
					require.Equal(t, []string{"https://updated.example.com:443"}, store.pair.EgressGrant.Destinations)
				})
				require.Equal(t, []database.RuntimeEgressSource{wantSource, wantSource}, store.sources)

				policies.DeleteObject("team-a/access")
				synctest.Wait()
				backend.inspect(func() {
					require.Empty(t, store.pair.DesiredRevision)
					require.Empty(t, store.pair.EgressGrant.Destinations)
					require.Zero(t, store.recordCalls, "grant changes must not require a compiled runtime")
				})
				require.Equal(t, []database.RuntimeEgressSource{wantSource, wantSource, wantSource}, store.sources, "revocation must apply actors while compilation remains broken")
			})
		})
	}
}

func TestAgentGrantWriteFailureAndRetirementApplyActors(t *testing.T) {
	collections, _ := newPreparationTestCollections(t, "gvisor")
	state := collections.Reconciliations.List()[0]
	store := &fakeRuntimeRevisionStore{pairErr: errors.New("response lost")}
	r := &Reconciler{collections: collections, store: store, status: kagentfake.NewSimpleClientset(state.Agent.DeepCopy()).ApiV1alpha3()}
	require.ErrorIs(t, r.reconcileAgent(t.Context(), state.ResourceName()), store.pairErr)
	require.Empty(t, store.sources, "a failed write retries preparation before applying actors")
	collections.Agents.(krt.StaticCollection[*v1alpha3.Agent]).DeleteObject(state.ResourceName())
	waitFor(t, func() bool { return collections.Reconciliations.GetKey(state.ResourceName()) == nil })
	require.NoError(t, r.reconcileAgent(t.Context(), state.ResourceName()))
	require.Equal(t, []database.RuntimeEgressSource{{Kind: database.RuntimeEgressAgent, Namespace: "team-a", Name: "assistant"}}, store.sources)
}

func TestAgentStartupRecoversOfflineDeletions(t *testing.T) {
	for _, namespaces := range [][]string{nil, {"team-a"}} {
		t.Run(fmt.Sprint(namespaces), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				collections, _ := newPreparationTestCollections(t, "gvisor")
				collections.watchNamespaces = namespaces
				// Delete before registering handlers: no KRT event can recover it.
				collections.Agents.(krt.StaticCollection[*v1alpha3.Agent]).DeleteObject("team-a/assistant")
				synctest.Wait()
				sources := []database.RuntimeEgressSource{
					{Kind: database.RuntimeEgressAgent, Namespace: "team-a", Name: "assistant"},
					{Kind: database.RuntimeEgressAgent, Namespace: "team-b", Name: "assistant"},
				}
				store := &fakeRuntimeRevisionStore{definitionSources: sources, egressTargetStoreStub: egressTargetStoreStub{
					targets: []database.RuntimeEgressTarget{{ID: "actor", Revision: "old", Atespace: "team-a", TemplateName: "runtime"}},
				}}
				attempts := 0
				actors := &fakeActorTemplates{actorEgressStub: actorEgressStub{ensurePolicy: func(context.Context, string, string, string) error {
					assert.NotEmpty(t, store.retired, "retire before applying revocation")
					attempts++
					if attempts == 1 {
						return errors.New("backend unavailable")
					}
					return nil
				}}}
				r := newReconciler(collections, actors, store, kagentfake.NewSimpleClientset().ApiV1alpha3())
				var workers sync.WaitGroup
				workers.Go(func() { assert.NoError(t, r.Start(ctx)) })
				defer func() { cancel(); workers.Wait() }()
				synctest.Wait()
				time.Sleep(time.Second)
				synctest.Wait()
				want := []database.RuntimeEgressSource{sources[0], sources[0]}
				if len(namespaces) == 0 {
					want = append(want, sources[1])
				}
				require.ElementsMatch(t, want, store.sources, "recovery must honor namespace scope and retry revocation")
				require.Equal(t, len(want), attempts)
				require.Zero(t, store.pairCalls, "missing Agents must not be recreated")
			})
		})
	}
}

func TestAgentStartupRecoveryReadFailure(t *testing.T) {
	collections, _ := newPreparationTestCollections(t, "gvisor")
	store := &fakeRuntimeRevisionStore{sourcesErr: errors.New("database unavailable")}
	r := newReconciler(collections, &fakeActorTemplates{}, store, kagentfake.NewSimpleClientset().ApiV1alpha3())
	require.ErrorIs(t, r.Start(t.Context()), store.sourcesErr)
	require.Zero(t, store.pairCalls, "failed recovery must fail startup")
}

func TestSandboxGrantsSurviveUnrelatedCompilationFailure(t *testing.T) {
	opts := krt.NewOptionsBuilder(t.Context().Done(), "test-grants", nil)
	template := sandboxTestTemplate()
	template.Finalizers = []string{sandboxPreparationFinalizer}
	template.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: "access"}}
	templates := krt.NewStaticCollection(nil, []*v1alpha3.SandboxTemplate{template}, opts.WithName("Templates")...)
	policy := &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "access"}, Spec: v1alpha3.EgressPolicySpec{Destinations: []v1alpha3.EgressDestination{{Origin: "https://tickets.example.com"}}}}
	policies := krt.NewStaticCollection(nil, []*v1alpha3.EgressPolicy{policy}, opts.WithName("Policies")...)
	pools := krt.NewStaticCollection[*atev1.WorkerPool](nil, nil, opts.WithName("Pools")...)
	store := &sandboxTestStore{}
	s := &SandboxReconciler{collections: newSandboxCollections(Collections{SandboxTemplates: templates, WorkerPools: pools, EgressPolicies: policies}, substrate.SandboxPolicy{}, opts), store: store, client: kagentfake.NewSimpleClientset(template.DeepCopy()).ApiV1alpha3()}
	waitFor(t, func() bool { return s.collections.states.GetKey("team-a/scratch") != nil })
	require.NoError(t, s.reconcile(t.Context(), "team-a/scratch"))
	require.Nil(t, s.collections.states.GetKey("team-a/scratch").DesiredActorTemplate, "missing WorkerPool prevents runtime compilation")
	require.Equal(t, []string{"https://tickets.example.com:443"}, store.desired.EgressGrant.Destinations)
	require.Len(t, store.sources, 1)
	policies.DeleteObject("team-a/access")
	waitFor(t, func() bool { return len(s.collections.states.GetKey("team-a/scratch").EgressIssues) != 0 })
	require.NoError(t, s.reconcile(t.Context(), "team-a/scratch"))
	require.Empty(t, store.desired.EgressGrant.Destinations)
	require.Len(t, store.sources, 2, "revocation must reach existing actors during failed compilation")
	store.upsertErr = errors.New("response lost")
	require.ErrorIs(t, s.reconcile(t.Context(), "team-a/scratch"), store.upsertErr)
	require.Len(t, store.sources, 2, "failed writes retry before actor application")
	template = template.DeepCopy()
	template.DeletionTimestamp = &metav1.Time{Time: metav1.Now().Time}
	templates.UpdateObject(template)
	waitFor(t, func() bool { return !s.collections.states.GetKey("team-a/scratch").Template.DeletionTimestamp.IsZero() })
	require.NoError(t, s.reconcile(t.Context(), "team-a/scratch"))
	require.Len(t, store.sources, 3)
}

// Registering the KRT handler must recover actors even when their grants already
// match persisted state. A failed push must retry without another graph event.
func TestPreparationReplaysAndRetriesUnchangedGrants(t *testing.T) {
	for _, kind := range []string{"agent", "sandbox"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				grants := egress.Access{Destinations: []string{"https://tickets.example.com:443"}}
				target := database.RuntimeEgressTarget{ID: "actor", Revision: "old", Atespace: "team-a", TemplateName: "runtime"}
				var start func(context.Context) error
				var persisted func() egress.Access
				var actorStub *actorEgressStub
				switch kind {
				case "agent":
					collections, harnesses := newPreparationTestCollections(t, "gvisor")
					agent := collections.Agents.List()[0].DeepCopy()
					agent.Spec.EgressPolicyRefs = []corev1.LocalObjectReference{{Name: "access"}}
					collections.EgressPolicies.(krt.StaticCollection[*v1alpha3.EgressPolicy]).UpdateObject(&v1alpha3.EgressPolicy{
						ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "access"}, Spec: v1alpha3.EgressPolicySpec{Destinations: []v1alpha3.EgressDestination{{Origin: grants.Destinations[0]}}},
					})
					collections.Agents.(krt.StaticCollection[*v1alpha3.Agent]).UpdateObject(agent)
					harnesses.DeleteObject("team-a/byo")
					synctest.Wait()
					store := &fakeRuntimeRevisionStore{pair: &database.AgentDefinition{EgressGrant: grants}, egressTargetStoreStub: egressTargetStoreStub{targets: []database.RuntimeEgressTarget{target}}}
					actors := &fakeActorTemplates{}
					backend := &preparationTestBackend{templates: actors, store: store}
					r := newReconciler(collections, backend, backend, kagentfake.NewSimpleClientset(agent.DeepCopy()).ApiV1alpha3())
					start = r.Start
					persisted = func() egress.Access { backend.mu.Lock(); defer backend.mu.Unlock(); return store.pair.EgressGrant }
					actorStub = &actors.actorEgressStub
				case "sandbox":
					opts := krt.NewOptionsBuilder(ctx.Done(), "retry-grants", nil)
					template := sandboxTestTemplate()
					template.Finalizers = []string{sandboxPreparationFinalizer}
					states := krt.NewStaticCollection(nil, []sandboxReconciliation{{Template: template, EgressGrant: grants}}, opts.WithName("states")...)
					store := &sandboxTestStore{desired: database.SandboxTemplateDefinition{EgressGrant: grants}, egressTargetStoreStub: egressTargetStoreStub{targets: []database.RuntimeEgressTarget{target}}}
					actors := &sandboxTestActors{}
					s := &SandboxReconciler{collections: sandboxCollections{states: states, observations: krt.NewStaticCollection[sandboxRuntimeObservation](nil, nil, opts.WithName("observations")...)}, store: store, actors: actors, client: kagentfake.NewSimpleClientset(template.DeepCopy()).ApiV1alpha3()}
					start = s.Start
					persisted = func() egress.Access { store.mu.Lock(); defer store.mu.Unlock(); return store.desired.EgressGrant }
					actorStub = &actors.actorEgressStub
				}
				var mu sync.Mutex
				var attempts []time.Time
				var applied egress.Access
				actorStub.ensurePolicy = func(context.Context, string, string, string) error {
					mu.Lock()
					defer mu.Unlock()
					attempts = append(attempts, time.Now())
					if len(attempts) == 1 {
						return errors.New("backend unavailable")
					}
					applied = persisted()
					return nil
				}
				var workers sync.WaitGroup
				workers.Go(func() { assert.NoError(t, start(ctx)) })
				defer func() { cancel(); workers.Wait() }()
				synctest.Wait()
				mu.Lock()
				require.Len(t, attempts, 1, "startup must attempt actors with unchanged grants")
				mu.Unlock()
				time.Sleep(time.Second)
				synctest.Wait()
				mu.Lock()
				defer mu.Unlock()
				require.Len(t, attempts, 2, "the preparation queue must retry a failed push without input changes")
				require.Equal(t, time.Second, attempts[1].Sub(attempts[0]))
				require.Equal(t, grants.Destinations, applied.Destinations)
			})
		})
	}
}

func TestSandboxFinalizerWaitsForActorRevocation(t *testing.T) {
	s, templates, _ := newSandboxTestReconciler(t, "")
	const key = "team-a/scratch"
	require.NoError(t, s.reconcile(t.Context(), key))
	template := syncSandboxTemplate(t, s, templates).DeepCopy()
	template.DeletionTimestamp = &metav1.Time{Time: metav1.Now().Time}
	templates.UpdateObject(template)
	waitFor(t, func() bool { return !s.collections.states.GetKey(key).Template.DeletionTimestamp.IsZero() })
	store := s.store.(*sandboxTestStore)
	store.targets = []database.RuntimeEgressTarget{{ID: "actor", Revision: "old", Atespace: "team-a", TemplateName: "runtime"}}
	pushErr := errors.New("backend unavailable")
	s.actors.(*sandboxTestActors).ensurePolicy = func(context.Context, string, string, string) error {
		assert.True(t, store.retired, "retire before applying revocation")
		return pushErr
	}
	require.ErrorIs(t, s.reconcile(t.Context(), key), pushErr)
	persisted, err := s.client.SandboxTemplates("team-a").Get(t.Context(), "scratch", metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, persisted.Finalizers, sandboxPreparationFinalizer)
	pushErr = nil
	require.NoError(t, s.reconcile(t.Context(), key))
	persisted, err = s.client.SandboxTemplates("team-a").Get(t.Context(), "scratch", metav1.GetOptions{})
	require.NoError(t, err)
	require.NotContains(t, persisted.Finalizers, sandboxPreparationFinalizer)
}
