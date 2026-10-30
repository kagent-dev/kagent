package runtimeauth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/actorsession"
)

type ActorSessionStore interface {
	GetSessionTurn(context.Context, string) (*apiv1.Session, bool, error)
}

// ActorSessions resolves session actors with the binding Session applies to runtime calls.
type ActorSessions struct{ store ActorSessionStore }

var _ actorsession.Reader = ActorSessions{}
var _ ActorSessionStore = (*database.Client)(nil)

func NewActorSessions(store ActorSessionStore) ActorSessions {
	return ActorSessions{store: store}
}

func (a ActorSessions) ActorSession(ctx context.Context, actor *ateapipb.ObjectRef) (actorsession.Session, error) {
	id, ok := strings.CutPrefix(actor.GetName(), "session-")
	if parsed, err := uuid.Parse(id); !ok || err != nil || parsed.String() != id {
		return actorsession.Session{}, actorsession.ErrNotFound
	}
	session, running, err := a.store.GetSessionTurn(ctx, id)
	if errors.Is(err, database.ErrNotFound) ||
		(err == nil && session.A2AAuthority != substrate.ActorHost(actor.GetAtespace(), substrate.ActorName(id), "")) {
		return actorsession.Session{}, actorsession.ErrNotFound
	}
	if err != nil {
		return actorsession.Session{}, fmt.Errorf("resolve actor session: %w", err)
	}
	return actorsession.Session{ID: session.Id, Active: running}, nil
}
