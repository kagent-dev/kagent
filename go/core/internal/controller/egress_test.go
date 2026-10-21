package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"go.ate.dev/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kagent-dev/kagent/go/core/internal/database"
)

type egressTargetStoreStub struct {
	sources []database.RuntimeEgressSource
	targets []database.RuntimeEgressTarget
	listErr error
}

var _ actorEgressStore = (*egressTargetStoreStub)(nil)

func (s *egressTargetStoreStub) ListRuntimeEgressSourceTargets(_ context.Context, source database.RuntimeEgressSource, after string, limit int) ([]database.RuntimeEgressTarget, error) {
	s.sources = append(s.sources, source)
	if s.listErr != nil {
		return nil, s.listErr
	}
	targets := slices.DeleteFunc(slices.Clone(s.targets), func(target database.RuntimeEgressTarget) bool { return target.ID <= after })
	return targets[:min(len(targets), limit)], nil
}

type actorEgressStub struct {
	getActor     func(context.Context, string, string) (*ateapipb.Actor, error)
	ensurePolicy func(context.Context, string, string, string) error
}

var _ actorEgressClient = (*actorEgressStub)(nil)

func (a *actorEgressStub) GetActor(ctx context.Context, space, name string) (*ateapipb.Actor, error) {
	if a.getActor != nil {
		return a.getActor(ctx, space, name)
	}
	return &ateapipb.Actor{Metadata: &ateapipb.ResourceMetadata{Atespace: space, Name: name, Uid: "uid"}, ActorTemplate: &ateapipb.ObjectRef{Atespace: space, Name: "runtime"}}, nil
}
func (a *actorEgressStub) EnsureActorEgressPolicy(ctx context.Context, space, name, revision string) error {
	return a.ensurePolicy(ctx, space, name, revision)
}

func TestSourceEgressAppliesEveryPageWithBoundedConcurrency(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := database.RuntimeEgressSource{Kind: database.RuntimeEgressAgent, Namespace: "team", Name: "source"}
		store := &egressTargetStoreStub{}
		for i := range 101 {
			store.targets = append(store.targets, database.RuntimeEgressTarget{ID: fmt.Sprintf("%03d", i), Revision: "revision", Namespace: "team", Atespace: "team", TemplateName: "runtime"})
		}
		var mu sync.Mutex
		active, peak := 0, 0
		applied := map[string]int{}
		backendErr := errors.New("backend unavailable")
		actors := &actorEgressStub{ensurePolicy: func(ctx context.Context, _, name, _ string) error {
			mu.Lock()
			active++
			peak = max(peak, active)
			applied[name]++
			mu.Unlock()
			time.Sleep(time.Second)
			mu.Lock()
			active--
			mu.Unlock()
			if name == "session-000" {
				return backendErr
			}
			return ctx.Err()
		}}
		require.ErrorIs(t, applySourceEgress(t.Context(), store, actors, source), backendErr)
		require.Len(t, applied, 101, "a failed actor must not block subsequent actors or pages")
		require.Greater(t, peak, 1)
		require.LessOrEqual(t, peak, 16)
		require.Equal(t, []database.RuntimeEgressSource{source, source}, store.sources)
		store.listErr = errors.New("database unavailable")
		require.ErrorIs(t, applySourceEgress(t.Context(), store, actors, source), store.listErr)
	})
}

func TestActorEgressSkipsStaleOrMissingActors(t *testing.T) {
	for _, test := range []struct {
		name      string
		uid       string
		template  string
		getErr    error
		applyErr  error
		wantCalls int
		wantErr   bool
	}{
		{name: "matching", uid: "uid", template: "runtime", wantCalls: 1},
		{name: "replaced template", uid: "uid", template: "replacement"},
		{name: "replaced actor", uid: "replacement", template: "runtime"},
		{name: "missing actor", getErr: status.Error(codes.NotFound, "gone")},
		{name: "deleted during apply", uid: "uid", template: "runtime", applyErr: status.Error(codes.NotFound, "gone"), wantCalls: 1},
		{name: "read failure", getErr: errors.New("unavailable"), wantErr: true},
		{name: "apply failure", uid: "uid", template: "runtime", applyErr: errors.New("unavailable"), wantCalls: 1, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			actors := &actorEgressStub{
				getActor: func(_ context.Context, space, name string) (*ateapipb.Actor, error) {
					return &ateapipb.Actor{Metadata: &ateapipb.ResourceMetadata{Atespace: space, Name: name, Uid: test.uid}, ActorTemplate: &ateapipb.ObjectRef{Atespace: space, Name: test.template}}, test.getErr
				},
				ensurePolicy: func(context.Context, string, string, string) error {
					calls++
					return test.applyErr
				},
			}
			err := applyActorEgress(t.Context(), actors, database.RuntimeEgressTarget{ID: "actor", Revision: "revision", Atespace: "team", TemplateName: "runtime", ActorUID: "uid"})
			if test.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.wantCalls, calls)
		})
	}
}
