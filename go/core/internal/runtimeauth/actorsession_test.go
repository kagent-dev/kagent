package runtimeauth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/pkg/actorsession"
)

type actorSessionStore struct {
	session *apiv1.Session
	running bool
	err     error
}

func (s actorSessionStore) GetSessionTurn(_ context.Context, id string) (*apiv1.Session, bool, error) {
	if s.err != nil {
		return nil, false, s.err
	}
	if s.session.Id != id {
		return nil, false, database.ErrNotFound
	}
	return s.session, s.running, nil
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
		{name: "running", actor: &ateapipb.ObjectRef{Atespace: "team-a", Name: "session-" + id},
			store: actorSessionStore{session: session, running: true}, want: actorsession.Session{ID: id, Active: true}},
		{name: "not running", actor: &ateapipb.ObjectRef{Atespace: "team-a", Name: "session-" + id},
			store: actorSessionStore{session: session}, want: actorsession.Session{ID: id}},
		{name: "not found", actor: &ateapipb.ObjectRef{Atespace: "team-a", Name: "session-" + uuid.NewString()},
			store: actorSessionStore{session: session, running: true}, wantErr: actorsession.ErrNotFound},
		{name: "wrong atespace", actor: &ateapipb.ObjectRef{Atespace: "team-b", Name: "session-" + id},
			store: actorSessionStore{session: session, running: true}, wantErr: actorsession.ErrNotFound},
		{name: "bad name", actor: &ateapipb.ObjectRef{Atespace: "team-a", Name: "sandbox-" + id},
			store: actorSessionStore{session: session, running: true}, wantErr: actorsession.ErrNotFound},
		{name: "non-canonical id", actor: &ateapipb.ObjectRef{Atespace: "team-a", Name: "session-" + strings.ReplaceAll(id, "-", "")},
			store: actorSessionStore{session: session, running: true}, wantErr: actorsession.ErrNotFound},
		{name: "nil ref", store: actorSessionStore{session: session, running: true}, wantErr: actorsession.ErrNotFound},
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
