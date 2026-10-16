package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

func noopEnqueueActors(database.RuntimeEgressSource) {}

type egressTestStore struct {
	mu       sync.Mutex
	policies map[string]egress.Policy
	targets  map[database.RuntimeEgressSource][]database.RuntimeEgressTarget
}

var _ actorEgressStore = (*egressTestStore)(nil)

func (e *egressTestStore) GetRuntimeEgressPolicy(_ context.Context, revision string) (egress.Policy, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	policy, ok := e.policies[revision]
	if !ok {
		return egress.Policy{}, database.ErrNotFound
	}
	return policy, nil
}
func (e *egressTestStore) ListRuntimeEgressSourceTargets(_ context.Context, source database.RuntimeEgressSource, after string, limit int) ([]database.RuntimeEgressTarget, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return egressTestPage(e.targets[source], after, limit), nil
}
func (e *egressTestStore) ListRuntimeEgressTargets(_ context.Context, after string, limit int) ([]database.RuntimeEgressTarget, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var targets []database.RuntimeEgressTarget
	for _, rows := range e.targets {
		targets = append(targets, rows...)
	}
	return egressTestPage(targets, after, limit), nil
}
func egressTestPage(targets []database.RuntimeEgressTarget, after string, limit int) []database.RuntimeEgressTarget {
	targets = slices.Clone(targets)
	slices.SortFunc(targets, func(a, b database.RuntimeEgressTarget) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	targets = slices.DeleteFunc(targets, func(target database.RuntimeEgressTarget) bool { return target.ID <= after })
	return targets[:min(len(targets), limit)]
}

type observingEgressActors struct {
	store      *egressTestStore
	mu         sync.Mutex
	applied    map[string]egress.Access
	attempts   map[string][]time.Time
	failures   int
	mismatched bool
}

var _ actorEgressClient = (*observingEgressActors)(nil)

func (a *observingEgressActors) GetActor(_ context.Context, space, name string) (*ateapipb.Actor, error) {
	template := "runtime"
	if a.mismatched {
		template = "replacement"
	}
	return &ateapipb.Actor{Metadata: &ateapipb.ResourceMetadata{Atespace: space, Name: name, Uid: "uid"}, ActorTemplate: &ateapipb.ObjectRef{Atespace: space, Name: template}}, nil
}
func (a *observingEgressActors) EnsureActorEgressPolicy(ctx context.Context, _, name, revision string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.attempts == nil {
		a.attempts = map[string][]time.Time{}
	}
	a.attempts[name] = append(a.attempts[name], time.Now())
	if a.failures > 0 {
		a.failures--
		return errors.New("backend unavailable")
	}
	policy, err := a.store.GetRuntimeEgressPolicy(ctx, revision)
	if err != nil {
		return err
	}
	if a.applied == nil {
		a.applied = map[string]egress.Access{}
	}
	a.applied[name] = policy.Access
	return nil
}

func TestActorEgressSourcePagingAndStartupRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		source := database.RuntimeEgressSource{Kind: database.RuntimeEgressAgent, Namespace: "team", Name: "source"}
		foreign := database.RuntimeEgressSource{Kind: database.RuntimeEgressSandbox, Namespace: "other", Name: "source"}
		store := &egressTestStore{policies: map[string]egress.Policy{"old": {}, "new": {}}, targets: map[database.RuntimeEgressSource][]database.RuntimeEgressTarget{}}
		for i := range 101 {
			revision := "old"
			if i%2 == 0 {
				revision = "new"
			}
			store.targets[source] = append(store.targets[source], database.RuntimeEgressTarget{ID: fmt.Sprintf("%03d", i), Revision: revision, Namespace: "team", Atespace: "team", TemplateName: "runtime"})
		}
		store.targets[foreign] = []database.RuntimeEgressTarget{{ID: "foreign", Revision: "new", Namespace: "other", Atespace: "other", TemplateName: "runtime"}}
		actors := &observingEgressActors{store: store}
		applier := NewActorEgressApplier(ctx, store, actors, []string{"team"})
		var workers sync.WaitGroup
		workers.Go(func() { require.NoError(t, applier.Start(ctx)) })
		defer func() { cancel(); workers.Wait() }()
		synctest.Wait()
		require.Len(t, actors.applied, 101, "startup recovery must traverse every page within watch scope")
		updated := egress.Access{Destinations: []string{"https://tickets.example.com:443"}}
		store.mu.Lock()
		store.policies["new"] = egress.Policy{Access: updated}
		store.mu.Unlock()
		applier.EnqueueSource(source)
		applier.EnqueueSource(foreign)
		synctest.Wait()
		for name, calls := range actors.attempts {
			require.Len(t, calls, 2, name)
		}
		require.Equal(t, updated, actors.applied["session-000"])
		require.Empty(t, actors.applied["session-001"].Destinations, "old UID revisions retain their independently resolved policy")
		require.NotContains(t, actors.applied, "session-foreign")
	})
}

func TestActorEgressRetriesAndRejectsStaleIdentity(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		source := database.RuntimeEgressSource{Kind: database.RuntimeEgressSandbox, Namespace: "team", Name: "scratch"}
		target := database.RuntimeEgressTarget{ID: "actor", Revision: "revision", Namespace: "team", Atespace: "team", TemplateName: "runtime"}
		store := &egressTestStore{policies: map[string]egress.Policy{"revision": {}}, targets: map[database.RuntimeEgressSource][]database.RuntimeEgressTarget{source: {target}}}
		actors := &observingEgressActors{store: store, failures: 2}
		applier := NewActorEgressApplier(ctx, store, actors, nil)
		var workers sync.WaitGroup
		workers.Go(func() { require.NoError(t, applier.Start(ctx)) })
		defer func() { cancel(); workers.Wait() }()
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		time.Sleep(2 * time.Second)
		synctest.Wait()
		attempts := actors.attempts["session-actor"]
		require.Len(t, attempts, 3)
		require.Equal(t, time.Second, attempts[1].Sub(attempts[0]))
		require.Equal(t, 2*time.Second, attempts[2].Sub(attempts[1]))
		actors.mismatched = true
		require.NoError(t, applier.reconcileActor(ctx, target))
		require.Len(t, actors.attempts["session-actor"], 3)
	})
}

func TestActorEgressWarningsAreLimitedPerRevision(t *testing.T) {
	var logs bytes.Buffer
	ctx := logging.IntoContext(t.Context(), slog.New(slog.NewTextHandler(&logs, nil)))
	store := &egressTestStore{policies: map[string]egress.Policy{
		"first": {WithheldGrants: errors.New("credential conflict")}, "second": {WithheldGrants: errors.New("rule limit")},
	}}
	actors := &observingEgressActors{store: store}
	applier := NewActorEgressApplier(ctx, store, actors, nil)
	defer applier.pages.ShutDownEarly()
	defer applier.updates.ShutDownEarly()
	defer applier.recovery.ShutDownEarly()
	for _, revision := range []string{"first", "first", "second"} {
		require.NoError(t, applier.reconcileActor(ctx, database.RuntimeEgressTarget{ID: revision, Revision: revision, Namespace: "team", Atespace: "team", TemplateName: "runtime"}))
	}
	require.Equal(t, 2, bytes.Count(logs.Bytes(), []byte("explicit egress grants withheld")))
	require.Len(t, actors.applied, 2, "warnings must not block fallback application")
}
