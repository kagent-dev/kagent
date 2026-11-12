// Package actorsession tells an embedding program which Session an actor serves.
package actorsession

import (
	"context"
	"errors"

	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
)

// ErrNotFound means the actor serves no live Session.
var ErrNotFound = errors.New("actor serves no session")

// Session is the Session an actor serves.
type Session struct {
	ID string
	// Active reports that the Session is running a turn.
	Active bool
}

// Reader resolves a Substrate actor to the Session it serves.
type Reader interface {
	ActorSession(context.Context, *ateapipb.ObjectRef) (Session, error)
}
