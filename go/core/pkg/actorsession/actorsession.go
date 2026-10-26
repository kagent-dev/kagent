// Package actorsession tells an embedding program which Session an actor serves.
package actorsession

import (
	"context"
	"errors"

	"go.ate.dev/substrate/pkg/proto/ateapipb"
)

// ErrNotFound means the actor serves no live Session.
var ErrNotFound = errors.New("actor serves no session")

// Session is the Session an actor serves.
type Session struct {
	ID string
	// Active reports that the Session runs a turn: an A2A task is submitted or
	// working, and no lifecycle operation or native cleanup is underway.
	Active bool
}

// Reader resolves a Substrate actor to the Session it serves.
type Reader interface {
	ActorSession(context.Context, *ateapipb.ObjectRef) (Session, error)
}
