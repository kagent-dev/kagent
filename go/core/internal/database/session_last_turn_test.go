package database

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Every turn's dispatch moves the session's last turn time, which get and list
// show; a session no turn reached has none.
func TestSessionLastTurnTimeMovesWithEveryTurn(t *testing.T) {
	client, session := expirationFixture(t)
	ctx := t.Context()
	require.Nil(t, session.GetLastTurnTime())

	var previous time.Time
	for _, message := range []string{"first", "second"} {
		before := time.Now()
		dispatch := uuid.New()
		require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, dispatch, message))
		read, err := client.GetSession(ctx, session.Id, session.Creator)
		require.NoError(t, err)
		lastTurn := read.GetLastTurnTime().AsTime()
		require.False(t, lastTurn.Before(before), "%s turn: last turn %s before its dispatch %s", message, lastTurn, before)
		require.True(t, lastTurn.After(previous), "%s turn moves the last turn time", message)
		listed, err := client.ListSessions(ctx, SessionQuery{UserID: session.Creator, Limit: 10})
		require.NoError(t, err)
		require.Len(t, listed, 1)
		require.True(t, listed[0].GetLastTurnTime().AsTime().Equal(lastTurn), "list shows the last turn time")
		_, err = client.RevokeSessionDispatch(ctx, session.Id, dispatch, message)
		require.NoError(t, err)
		previous = lastTurn
	}
}
