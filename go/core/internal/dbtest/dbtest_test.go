package dbtest_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
)

func TestStartTTerminatesContainerAfterTestContextIsCanceled(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	var connStr string
	require.True(t, t.Run("fixture", func(t *testing.T) {
		// t.Context is canceled before the fixture's cleanup runs.
		connStr = dbtest.StartT(t.Context(), t)
		conn, err := pgx.Connect(t.Context(), connStr)
		require.NoError(t, err)
		require.NoError(t, conn.Close(t.Context()))
	}))

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, err := pgx.Connect(ctx, connStr)
	require.Error(t, err, "postgres container still accepts connections after cleanup")
}
