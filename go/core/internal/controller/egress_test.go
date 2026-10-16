package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/cache"
	clocktesting "k8s.io/utils/clock/testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

func noopEnqueueEgress(string) {}

type egressTestStore struct {
	mu                  sync.Mutex
	pinned              map[string]database.RuntimeEgress
	intended            map[string]egress.Access
	targets             map[string][]database.RuntimeEgressTarget
	listed              chan database.RuntimeEgressSource
	publishErr          error
	losePublishResponse bool
	beforePublish       func(context.Context, string, egress.Access) error
}

var _ runtimeEgressStore = (*egressTestStore)(nil)

func (e *egressTestStore) GetRuntimeEgress(_ context.Context, revision string) (*database.RuntimeEgress, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	row, ok := e.pinned[revision]
	if !ok {
		return nil, database.ErrNotFound
	}
	return &row, nil
}
func (e *egressTestStore) SetRuntimeEgressPolicy(ctx context.Context, revision string, access egress.Access) (bool, error) {
	if e.beforePublish != nil {
		if err := e.beforePublish(ctx, revision, access); err != nil {
			return false, err
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.publishErr != nil {
		return false, e.publishErr
	}
	if e.intended == nil {
		e.intended = map[string]egress.Access{}
	}
	if current, exists := e.intended[revision]; exists && reflect.DeepEqual(current, access) {
		return false, nil
	}
	e.intended[revision] = access
	if e.losePublishResponse {
		e.losePublishResponse = false
		return false, errors.New("lost publication response")
	}
	return true, nil
}
func (e *egressTestStore) GetRuntimeEgressPolicy(_ context.Context, revision string) (egress.Access, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	policy, ok := e.intended[revision]
	if !ok {
		return egress.Access{}, database.ErrNotFound
	}
	return policy, nil
}
func (e *egressTestStore) ListRuntimeEgressRevisions(_ context.Context, after string, limit int) ([]string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var revisions []string
	for key := range e.pinned {
		if key > after {
			revisions = append(revisions, key)
		}
	}
	slices.Sort(revisions)
	return revisions[:min(limit, len(revisions))], nil
}
func (e *egressTestStore) ListRuntimeEgressSourceRevisions(_ context.Context, source database.RuntimeEgressSource, after string, limit int) ([]string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.listed != nil {
		e.listed <- source
	}
	var revisions []string
	for key, pinned := range e.pinned {
		if pinned.Kind == source.Kind && pinned.Namespace == source.Namespace && pinned.SourceName == source.Name && key > after {
			revisions = append(revisions, key)
		}
	}
	slices.Sort(revisions)
	return revisions[:min(limit, len(revisions))], nil
}
func (e *egressTestStore) ListRuntimeEgressTargets(_ context.Context, revision, after string, limit int) ([]database.RuntimeEgressTarget, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var targets []database.RuntimeEgressTarget
	for _, target := range e.targets[revision] {
		if target.ID > after {
			targets = append(targets, target)
		}
	}
	return targets[:min(limit, len(targets))], nil
}

type egressTestInputs struct {
	runtime   *Runtime
	agents    krt.StaticCollection[*v1alpha3.Agent]
	sandboxes krt.StaticCollection[*v1alpha3.SandboxTemplate]
	policies  krt.StaticCollection[*v1alpha3.EgressPolicy]
	secrets   krt.StaticCollection[*corev1.Secret]
}

func newEgressTestInputs(ctx context.Context) egressTestInputs {
	opts := krt.NewOptionsBuilder(ctx.Done(), "test", nil)
	inputs := egressTestInputs{
		agents:    krt.NewStaticCollection[*v1alpha3.Agent](nil, nil, opts.WithName("Agents")...),
		sandboxes: krt.NewStaticCollection[*v1alpha3.SandboxTemplate](nil, nil, opts.WithName("Sandboxes")...),
		policies:  krt.NewStaticCollection[*v1alpha3.EgressPolicy](nil, nil, opts.WithName("Policies")...),
		secrets:   krt.NewStaticCollection[*corev1.Secret](nil, nil, opts.WithName("Secrets")...),
	}
	inputs.runtime = &Runtime{Options: opts, Collections: Collections{Agents: inputs.agents, SandboxTemplates: inputs.sandboxes, EgressPolicies: inputs.policies, Secrets: inputs.secrets}}
	return inputs
}
func (i egressTestInputs) source(kind database.RuntimeEgressSourceKind, uid string, refs []corev1.LocalObjectReference) {
	metadata := metav1.ObjectMeta{Namespace: "team", Name: "source", UID: types.UID(uid)}
	if kind == database.RuntimeEgressAgent {
		i.agents.UpdateObject(&v1alpha3.Agent{ObjectMeta: metadata, Spec: v1alpha3.AgentSpec{EgressPolicyRefs: refs}})
	} else {
		i.sandboxes.UpdateObject(&v1alpha3.SandboxTemplate{ObjectMeta: metadata, Spec: v1alpha3.SandboxTemplateSpec{EgressPolicyRefs: refs}})
	}
}
func egressTestPolicy(origin string, auth *v1alpha3.EgressAuthentication) *v1alpha3.EgressPolicy {
	return &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "access"}, Spec: v1alpha3.EgressPolicySpec{Destinations: []v1alpha3.EgressDestination{{Origin: origin, Authentication: auth}}}}
}

func TestEgressTranslationUsesCachedDependencies(t *testing.T) {
	for _, kind := range []database.RuntimeEgressSourceKind{database.RuntimeEgressAgent, database.RuntimeEgressSandbox} {
		t.Run(string(kind), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			inputs := newEgressTestInputs(ctx)
			refs := []corev1.LocalObjectReference{{Name: "access"}}
			inputs.source(kind, "original", refs)
			base := egress.Access{Destinations: []string{"https://model.example.com:443"}}
			store := &egressTestStore{pinned: map[string]database.RuntimeEgress{"pinned": {Kind: kind, Namespace: "team", SourceName: "source", SourceUID: "original", EgressDestinations: base.Destinations}}}
			controller := NewEgressReconciler(ctx, inputs.runtime, store, &observingEgressActors{store: store}, nil)
			done := make(chan error, 1)
			go func() { done <- controller.Start(ctx) }()
			defer func() { cancel(); require.NoError(t, <-done) }()
			expect := func(want egress.Access) {
				t.Helper()
				require.Eventually(t, func() bool {
					got, err := store.GetRuntimeEgressPolicy(ctx, "pinned")
					return err == nil && reflect.DeepEqual(want, got)
				}, time.Second, 10*time.Millisecond)
			}
			expect(base) // Missing refs and broken unrelated runtime inputs cannot preserve stale grants.
			policy := egressTestPolicy("https://tickets.example.com", nil)
			inputs.policies.UpdateObject(policy.DeepCopy())
			allowed := egress.Access{Destinations: []string{"https://model.example.com:443", "https://tickets.example.com:443"}}
			expect(allowed)
			inputs.source(kind, "original", nil)
			expect(base)
			inputs.source(kind, "original", refs)
			expect(allowed)
			inputs.policies.DeleteObject("team/access")
			expect(base)
			inputs.policies.UpdateObject(policy.DeepCopy())
			expect(allowed)
			inputs.source(kind, "replacement", refs)
			expect(base)
			inputs.source(kind, "original", refs)
			expect(allowed)
			policy.Spec.Destinations[0].Authentication = &v1alpha3.EgressAuthentication{Header: "authorization", SecretKeyRef: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "auth"}, Key: "token"}}
			inputs.policies.UpdateObject(policy.DeepCopy())
			expect(base)
			inputs.secrets.UpdateObject(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "auth"}, Data: map[string][]byte{"token": []byte("secret")}})
			allowed.Credentials = []egress.Credential{{Origin: "https://tickets.example.com:443", Header: "authorization", URI: "ate-secret://k8s.io/default/team/auth/token"}}
			expect(allowed)
			inputs.secrets.DeleteObject("team/auth")
			expect(base)
		})
	}
}

type observingEgressActors struct {
	store    *egressTestStore
	mu       sync.Mutex
	applied  map[string]egress.Access
	attempts map[string][]time.Time
	failing  string
	failures int
}

func (a *observingEgressActors) EnsureActorEgressPolicy(ctx context.Context, _, name, revision string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.attempts == nil {
		a.attempts = map[string][]time.Time{}
	}
	a.attempts[name] = append(a.attempts[name], time.Now())
	if name == a.failing && len(a.attempts[name]) <= a.failures {
		return errors.New("backend unavailable")
	}
	access, err := a.store.GetRuntimeEgressPolicy(ctx, revision)
	if err != nil {
		return err
	}
	if a.applied == nil {
		a.applied = map[string]egress.Access{}
	}
	a.applied[name] = access
	return nil
}
func (a *observingEgressActors) GetActor(_ context.Context, space, name string) (*ateapipb.Actor, error) {
	return &ateapipb.Actor{Metadata: &ateapipb.ResourceMetadata{Atespace: space, Name: name, Uid: "uid"}, ActorTemplate: &ateapipb.ObjectRef{Atespace: space, Name: "runtime"}}, nil
}

func TestEgressUpdatesRetainedRevisionsAndRecoversAfterRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	inputs := newEgressTestInputs(ctx)
	inputs.source(database.RuntimeEgressAgent, "uid", []corev1.LocalObjectReference{{Name: "access"}})
	inputs.policies.UpdateObject(egressTestPolicy("https://tickets.example.com", nil))
	pinned := database.RuntimeEgress{Kind: database.RuntimeEgressAgent, Namespace: "team", SourceName: "source", SourceUID: "uid"}
	old := pinned
	old.EgressDestinations = []string{"https://old-model.example.com:443"}
	store := &egressTestStore{pinned: map[string]database.RuntimeEgress{"new": pinned, "old": old}, targets: map[string][]database.RuntimeEgressTarget{
		"new": {{ID: "new", Revision: "new", Atespace: "team", TemplateName: "runtime"}},
		"old": {{ID: "old", Revision: "old", Atespace: "team", TemplateName: "runtime"}},
	}}
	actors := &observingEgressActors{store: store}
	expect := func(granted bool) {
		t.Helper()
		require.Eventually(t, func() bool {
			actors.mu.Lock()
			defer actors.mu.Unlock()
			current, ok := actors.applied["session-new"]
			previous, oldOK := actors.applied["session-old"]
			return ok && oldOK && slices.Contains(current.Destinations, "https://tickets.example.com:443") == granted &&
				slices.Contains(previous.Destinations, "https://tickets.example.com:443") == granted && slices.Contains(previous.Destinations, "https://old-model.example.com:443")
		}, 3*time.Second, 10*time.Millisecond)
	}
	for i := range 2 {
		runCtx, stop := context.WithCancel(ctx)
		controller := NewEgressReconciler(runCtx, inputs.runtime, store, actors, nil)
		done := make(chan error, 1)
		go func() { done <- controller.Start(runCtx) }()
		if i == 0 {
			expect(true)
			inputs.policies.DeleteObject("team/access")
			expect(false)
		} else {
			expect(false)
		}
		stop()
		require.NoError(t, <-done)
		// Startup repairs existing actors without a new Kubernetes event/invocation.
		actors.mu.Lock()
		actors.applied = map[string]egress.Access{}
		actors.mu.Unlock()
	}
}

func TestEgressSkipsUnchangedPoliciesAndRecoversLostWriteResponse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		inputs := newEgressTestInputs(ctx)
		inputs.source(database.RuntimeEgressAgent, "uid", []corev1.LocalObjectReference{{Name: "access"}})
		inputs.policies.UpdateObject(egressTestPolicy("https://tickets.example.com", nil))
		store := &egressTestStore{
			pinned:              map[string]database.RuntimeEgress{"revision": {Kind: database.RuntimeEgressAgent, Namespace: "team", SourceName: "source", SourceUID: "uid"}},
			targets:             map[string][]database.RuntimeEgressTarget{"revision": {{ID: "actor", Revision: "revision", Atespace: "team", TemplateName: "runtime"}}},
			losePublishResponse: true,
		}
		actors := &observingEgressActors{store: store}
		controller := NewEgressReconciler(ctx, inputs.runtime, store, actors, nil)
		defer controller.sources.ShutDownEarly()
		var workers sync.WaitGroup
		for _, queue := range []controllers.Queue{controller.pages, controller.updates, controller.recovery, controller.sweeps} {
			workers.Go(func() { queue.Run(ctx.Done()) })
		}
		workers.Go(func() { controller.runPublications(ctx) })
		defer func() { cancel(); workers.Wait() }()
		require.True(t, controller.grants.WaitUntilSynced(ctx.Done()))
		controller.EnqueueRevision("revision")
		synctest.Wait()
		require.Len(t, actors.attempts["session-actor"], 1, "an uncertain write must still apply the persisted policy")
		require.Contains(t, actors.applied["session-actor"].Destinations, "https://tickets.example.com:443")
		for range 3 {
			controller.EnqueueRevision("revision")
			controller.sweeps.Add(egressSweepPage{})
			synctest.Wait()
		}
		require.Len(t, actors.attempts["session-actor"], 1, "unchanged preparation and sweeps must not touch actors")
		inputs.policies.DeleteObject("team/access")
		synctest.Wait()
		controller.sweeps.Add(egressSweepPage{})
		synctest.Wait()
		require.Len(t, actors.attempts["session-actor"], 2, "a sweep must repair a missed policy event")
		require.Empty(t, actors.applied["session-actor"].Destinations)
		controller.sweeps.Add(egressSweepPage{RecoverActors: true})
		synctest.Wait()
		require.Len(t, actors.attempts["session-actor"], 3, "startup must also check actors with unchanged policies")
	})
}

func TestEgressStartupRecoveryRespectsWatchNamespaces(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		inputs := newEgressTestInputs(ctx)
		store := &egressTestStore{
			pinned: map[string]database.RuntimeEgress{
				"watched":   {Kind: database.RuntimeEgressAgent, Namespace: "team"},
				"unwatched": {Kind: database.RuntimeEgressAgent, Namespace: "other"},
			},
			intended: map[string]egress.Access{
				"watched":   {},
				"unwatched": {Destinations: []string{"https://tickets.example.com:443"}},
			},
			targets: map[string][]database.RuntimeEgressTarget{
				"watched":   {{ID: "watched", Revision: "watched", Atespace: "team", TemplateName: "runtime"}},
				"unwatched": {{ID: "unwatched", Revision: "unwatched", Atespace: "other", TemplateName: "runtime"}},
			},
		}
		actors := &observingEgressActors{store: store}
		controller := NewEgressReconciler(ctx, inputs.runtime, store, actors, []string{"team"})
		var workers sync.WaitGroup
		workers.Go(func() { require.NoError(t, controller.Start(ctx)) })
		defer func() { cancel(); workers.Wait() }()
		synctest.Wait()
		require.Contains(t, actors.applied, "session-watched")
		require.NotContains(t, actors.attempts, "session-unwatched")
		unwatched, err := store.GetRuntimeEgressPolicy(ctx, "unwatched")
		require.NoError(t, err)
		require.Equal(t, []string{"https://tickets.example.com:443"}, unwatched.Destinations)
	})
}

func TestEgressPublicationRequeuesRevocationDuringWrite(t *testing.T) {
	for _, failFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("fail_first=%t", failFirst), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				inputs := newEgressTestInputs(ctx)
				inputs.source(database.RuntimeEgressAgent, "uid", []corev1.LocalObjectReference{{Name: "access"}})
				inputs.policies.UpdateObject(egressTestPolicy("https://tickets.example.com", nil))
				pinned := database.RuntimeEgress{Kind: database.RuntimeEgressAgent, Namespace: "team", SourceName: "source", SourceUID: "uid"}
				entered, release := make(chan struct{}), make(chan struct{})
				store := &egressTestStore{
					pinned: map[string]database.RuntimeEgress{"revision": pinned, "other": pinned},
					beforePublish: func(ctx context.Context, revision string, access egress.Access) error {
						if revision != "revision" || len(access.Destinations) == 0 {
							return nil
						}
						close(entered)
						select {
						case <-release:
						case <-ctx.Done():
							return ctx.Err()
						}
						if failFirst {
							return errors.New("publication failed")
						}
						return nil
					},
				}
				controller := NewEgressReconciler(ctx, inputs.runtime, store, &observingEgressActors{store: store}, nil)
				var workers sync.WaitGroup
				workers.Go(func() { controller.runPublications(ctx) })
				defer func() {
					cancel()
					workers.Wait()
					for _, queue := range []controllers.Queue{controller.sources, controller.pages, controller.updates, controller.recovery, controller.sweeps} {
						queue.ShutDownEarly()
					}
				}()
				require.True(t, controller.grants.WaitUntilSynced(ctx.Done()))
				controller.EnqueueRevision("revision")
				<-entered
				inputs.policies.DeleteObject("team/access")
				synctest.Wait()
				controller.EnqueueRevision("revision")
				controller.EnqueueRevision("other")
				synctest.Wait()
				_, err := store.GetRuntimeEgressPolicy(ctx, "other")
				require.NoError(t, err, "another revision must publish while the first database write is blocked")
				_, err = store.GetRuntimeEgressPolicy(ctx, "revision")
				require.ErrorIs(t, err, database.ErrNotFound, "the same revision must wait through commit")
				close(release)
				synctest.Wait()
				for _, revision := range []string{"revision", "other"} {
					access, err := store.GetRuntimeEgressPolicy(ctx, revision)
					require.NoError(t, err)
					require.Empty(t, access.Destinations, "a dirty revision must rerun with the revoked grants")
				}
			})
		})
	}
}

func TestEgressPublicationRetriesWithoutAnotherEvent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		inputs := newEgressTestInputs(ctx)
		store := &egressTestStore{
			pinned:     map[string]database.RuntimeEgress{"revision": {Kind: database.RuntimeEgressAgent, Namespace: "team"}},
			publishErr: errors.New("database unavailable"),
		}
		controller := NewEgressReconciler(ctx, inputs.runtime, store, &observingEgressActors{store: store}, nil)
		var workers sync.WaitGroup
		workers.Go(func() { require.NoError(t, controller.Start(ctx)) })
		defer func() { cancel(); workers.Wait() }()
		synctest.Wait()
		_, err := store.GetRuntimeEgressPolicy(ctx, "revision")
		require.ErrorIs(t, err, database.ErrNotFound)
		store.mu.Lock()
		store.publishErr = nil
		store.mu.Unlock()
		time.Sleep(time.Second)
		synctest.Wait()
		_, err = store.GetRuntimeEgressPolicy(ctx, "revision")
		require.NoError(t, err)
	})
}

func TestEgressPostCommitEnqueueRecoversEventBeforeRevisionExists(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		inputs := newEgressTestInputs(ctx)
		inputs.source(database.RuntimeEgressAgent, "uid", []corev1.LocalObjectReference{{Name: "access"}})
		inputs.policies.UpdateObject(egressTestPolicy("https://tickets.example.com", nil))
		store := &egressTestStore{listed: make(chan database.RuntimeEgressSource, 1)}
		controller := NewEgressReconciler(ctx, inputs.runtime, store, &observingEgressActors{store: store}, nil)
		require.True(t, controller.grants.WaitUntilSynced(ctx.Done()))
		var workers sync.WaitGroup
		workers.Go(func() { require.NoError(t, controller.Start(ctx)) })
		defer func() { cancel(); workers.Wait() }()
		synctest.Wait()
		// Compilation saw the grant, but its revision has not committed yet.
		initial := controller.grants.GetKey("agent/team/source").Access
		require.NotEmpty(t, initial.Destinations)
		inputs.policies.DeleteObject("team/access")
		synctest.Wait()
		require.Len(t, store.listed, 1, "revocation discovery must run before the revision exists")
		require.Equal(t, database.RuntimeEgressSource{Kind: database.RuntimeEgressAgent, Namespace: "team", Name: "source"}, <-store.listed)
		store.mu.Lock()
		store.pinned = map[string]database.RuntimeEgress{"revision": {Kind: database.RuntimeEgressAgent, Namespace: "team", SourceName: "source", SourceUID: "uid"}}
		store.intended = map[string]egress.Access{"revision": initial}
		store.mu.Unlock()
		controller.EnqueueRevision("revision")
		synctest.Wait()
		policy, err := store.GetRuntimeEgressPolicy(ctx, "revision")
		require.NoError(t, err)
		require.Empty(t, policy.Destinations, "post-commit publication must apply the missed revocation")
	})
}

func TestEgressTargetsSourceChangesAndBacksOffActorFailures(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	inputs := newEgressTestInputs(ctx)
	inputs.source(database.RuntimeEgressAgent, "uid", nil)
	store := &egressTestStore{pinned: map[string]database.RuntimeEgress{"revision": {Kind: database.RuntimeEgressAgent, Namespace: "team", SourceName: "source", SourceUID: "uid"}},
		intended: map[string]egress.Access{"revision": {}},
		listed:   make(chan database.RuntimeEgressSource, 20), targets: map[string][]database.RuntimeEgressTarget{"revision": {
			{ID: "failing", Revision: "revision", Atespace: "team", TemplateName: "runtime"},
			{ID: "healthy", Revision: "revision", Atespace: "team", TemplateName: "runtime"},
		}}}
	actors := &observingEgressActors{store: store, failing: "session-failing", failures: 2}
	controller := NewEgressReconciler(ctx, inputs.runtime, store, actors, nil)
	// Exercise recovery backoff independently of initial informer delivery.
	require.True(t, controller.grants.WaitUntilSynced(ctx.Done()))
	done := make(chan error, 1)
	go func() { done <- controller.Start(ctx) }()
	defer func() { cancel(); require.NoError(t, <-done) }()
	require.Eventually(t, func() bool {
		actors.mu.Lock()
		defer actors.mu.Unlock()
		return len(actors.attempts["session-failing"]) == 3
	}, 5*time.Second, 10*time.Millisecond)
	actors.mu.Lock()
	attempts := slices.Clone(actors.attempts["session-failing"])
	healthy := len(actors.attempts["session-healthy"])
	actors.mu.Unlock()
	require.GreaterOrEqual(t, attempts[1].Sub(attempts[0]), time.Second)
	require.GreaterOrEqual(t, attempts[2].Sub(attempts[1]), 2*time.Second)
	require.Equal(t, 1, healthy)
	inputs.agents.UpdateObject(&v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "unrelated", UID: "other"}})
	source := database.RuntimeEgressSource{Kind: database.RuntimeEgressAgent, Namespace: "team", Name: "unrelated"}
	select {
	case got := <-store.listed:
		require.Equal(t, source, got)
	case <-ctx.Done():
		t.Fatal("source change was not queued")
	}
	inputs.agents.DeleteObject("team/unrelated")
	select {
	case got := <-store.listed:
		require.Equal(t, source, got)
	case <-ctx.Done():
		t.Fatal("source deletion was not queued")
	}
}

func TestEgressWithholdsConflictingGrantsAndWarns(t *testing.T) {
	for _, reason := range []string{"credential", "passthrough", "rule limit"} {
		t.Run(reason, func(t *testing.T) {
			var logs bytes.Buffer
			ctx := logging.IntoContext(t.Context(), slog.New(slog.NewJSONHandler(&logs, nil)))
			inputs := newEgressTestInputs(ctx)
			inputs.source(database.RuntimeEgressAgent, "uid", []corev1.LocalObjectReference{{Name: "access"}})
			base := egress.Access{Destinations: []string{"https://model.example.com:443"}}
			pinned := database.RuntimeEgress{Kind: database.RuntimeEgressAgent, Namespace: "team", SourceName: "source", SourceUID: "uid"}
			authentication := &v1alpha3.EgressAuthentication{Header: "authorization", SecretKeyRef: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "other"}, Key: "token"}}
			switch reason {
			case "credential":
				base.Credentials = []egress.Credential{{Origin: base.Destinations[0], Header: "authorization", URI: "ate-secret://k8s.io/default/team/auth/token"}}
			case "passthrough":
				pinned.TokenPassthroughOrigins = base.Destinations
			case "rule limit":
				base.Destinations = nil
				for i := range 256 {
					base.Destinations = append(base.Destinations, fmt.Sprintf("https://host-%03d.example.com:443", i))
				}
				authentication = nil
			}
			pinned.EgressDestinations, pinned.Credentials = base.Destinations, base.Credentials
			inputs.policies.UpdateObject(egressTestPolicy("https://model.example.com", authentication))
			inputs.secrets.UpdateObject(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "other"}, Data: map[string][]byte{"token": []byte("private-value")}})
			store := &egressTestStore{pinned: map[string]database.RuntimeEgress{"revision": pinned, "other": pinned}}
			controller := NewEgressReconciler(ctx, inputs.runtime, store, &observingEgressActors{store: store}, nil)
			defer controller.publications.ShutDown()
			defer func() {
				for _, queue := range []controllers.Queue{controller.sources, controller.pages, controller.updates, controller.recovery, controller.sweeps} {
					queue.ShutDownEarly()
				}
			}()
			require.True(t, controller.grants.WaitUntilSynced(ctx.Done()))
			publish := func(revision string) {
				controller.EnqueueRevision(revision)
				require.True(t, controller.processPublication(ctx))
			}
			clock := clocktesting.NewFakeClock(time.Now())
			controller.warnings = cache.NewLRUExpireCacheWithClock(1024, clock)
			for range 3 {
				publish("revision")
				got, err := store.GetRuntimeEgressPolicy(ctx, "revision")
				require.NoError(t, err)
				require.Equal(t, base, got)
			}
			require.Equal(t, 1, strings.Count(logs.String(), "explicit egress grants withheld"))
			publish("other")
			require.Contains(t, logs.String(), `"revision":"other"`, "a noisy revision must not suppress another revision's warning")
			require.Equal(t, 2, strings.Count(logs.String(), "explicit egress grants withheld"))
			clock.Step(31 * time.Second)
			publish("revision")
			require.Equal(t, 3, strings.Count(logs.String(), "explicit egress grants withheld"))
			require.Contains(t, logs.String(), `"level":"WARN"`)
			require.Contains(t, logs.String(), `"revision":"revision"`)
			require.NotContains(t, logs.String(), "private-value")
		})
	}
}
