package runtimeauth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.ate.dev/substrate/pkg/proto/ateapipb"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/pkg/actorsession"
)

type actorSessionStore struct {
	session *apiv1.Session
	active  a2a.TaskState
	err     error
}

func (s actorSessionStore) GetSessionByID(_ context.Context, id string) (*apiv1.Session, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.session.Id != id {
		return nil, database.ErrNotFound
	}
	return s.session, nil
}

func (s actorSessionStore) ListSessionTasks(_ context.Context, id, afterID string, state a2a.TaskState, after *time.Time, limit int, historyLength *int) ([]*a2a.Task, int, error) {
	if id != s.session.Id || afterID != "" || (state != a2a.TaskStateSubmitted && state != a2a.TaskStateWorking) ||
		after != nil || limit != 1 || historyLength == nil || *historyLength != 0 {
		return nil, 0, errors.New("unexpected task query")
	}
	if state == s.active {
		return nil, 1, nil
	}
	return nil, 0, nil
}

func TestActorSessionResolvesSessionActors(t *testing.T) {
	id := uuid.NewString()
	session := &apiv1.Session{Id: id, A2AAuthority: "session-" + id + ".team-a.actors.resources.substrate.ate.dev"}
	unavailable := errors.New("database unavailable")
	for _, test := range []struct {
		name    string
		actor   *ateapipb.ObjectRef
		store   actorSessionStore
		want    actorsession.Session
		wantErr error
	}{
		{name: "submitted", actor: &ateapipb.ObjectRef{Atespace: "team-a", Name: "session-" + id},
			store: actorSessionStore{session: session, active: a2a.TaskStateSubmitted}, want: actorsession.Session{ID: id, Active: true}},
		{name: "working", actor: &ateapipb.ObjectRef{Atespace: "team-a", Name: "session-" + id},
			store: actorSessionStore{session: session, active: a2a.TaskStateWorking}, want: actorsession.Session{ID: id, Active: true}},
		{name: "not active", actor: &ateapipb.ObjectRef{Atespace: "team-a", Name: "session-" + id},
			store: actorSessionStore{session: session}, want: actorsession.Session{ID: id}},
		{name: "not found", actor: &ateapipb.ObjectRef{Atespace: "team-a", Name: "session-" + uuid.NewString()},
			store: actorSessionStore{session: session, active: a2a.TaskStateWorking}, wantErr: actorsession.ErrNotFound},
		{name: "wrong atespace", actor: &ateapipb.ObjectRef{Atespace: "team-b", Name: "session-" + id},
			store: actorSessionStore{session: session, active: a2a.TaskStateWorking}, wantErr: actorsession.ErrNotFound},
		{name: "bad name", actor: &ateapipb.ObjectRef{Atespace: "team-a", Name: "sandbox-" + id},
			store: actorSessionStore{session: session, active: a2a.TaskStateWorking}, wantErr: actorsession.ErrNotFound},
		{name: "nil ref", store: actorSessionStore{session: session, active: a2a.TaskStateWorking}, wantErr: actorsession.ErrNotFound},
		{name: "store failure", actor: &ateapipb.ObjectRef{Atespace: "team-a", Name: "session-" + id},
			store: actorSessionStore{session: session, err: unavailable}, wantErr: unavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := NewActorSessions(test.store).ActorSession(t.Context(), test.actor)
			if test.wantErr != nil {
				require.ErrorIs(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}
