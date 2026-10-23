package runtimeauth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"go.ate.dev/substrate/pkg/proto/ateapipb"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/actorsession"
)

type ActorSessionStore interface {
	GetSessionByID(context.Context, string) (*apiv1.Session, error)
	ListSessionTasks(context.Context, string, string, a2a.TaskState, *time.Time, int, *int) ([]*a2a.Task, int, error)
}

// ActorSessions resolves session actors with the binding Session applies to runtime calls.
type ActorSessions struct{ store ActorSessionStore }

var _ actorsession.Reader = ActorSessions{}

func NewActorSessions(store ActorSessionStore) ActorSessions {
	return ActorSessions{store: store}
}

func (a ActorSessions) ActorSession(ctx context.Context, actor *ateapipb.ObjectRef) (actorsession.Session, error) {
	id, ok := strings.CutPrefix(actor.GetName(), "session-")
	if _, err := uuid.Parse(id); !ok || err != nil {
		return actorsession.Session{}, actorsession.ErrNotFound
	}
	session, err := a.store.GetSessionByID(ctx, id)
	if errors.Is(err, database.ErrNotFound) ||
		(err == nil && session.A2AAuthority != substrate.ActorHost(actor.GetAtespace(), substrate.ActorName(id), "")) {
		return actorsession.Session{}, actorsession.ErrNotFound
	}
	if err != nil {
		return actorsession.Session{}, err
	}
	// Runtimes start native work after the submitted save, before the working one lands.
	zero := 0
	for _, state := range []a2a.TaskState{a2a.TaskStateSubmitted, a2a.TaskStateWorking} {
		_, total, err := a.store.ListSessionTasks(ctx, session.Id, "", state, nil, 1, &zero)
		if errors.Is(err, database.ErrNotFound) {
			return actorsession.Session{}, actorsession.ErrNotFound
		}
		if err != nil {
			return actorsession.Session{}, err
		}
		if total > 0 {
			return actorsession.Session{ID: session.Id, Active: true}, nil
		}
	}
	return actorsession.Session{ID: session.Id}, nil
}
