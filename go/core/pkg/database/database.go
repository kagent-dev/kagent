// Package database exposes the supported way to construct kagent's persistence
// client.
//
// The implementation lives in core/internal/database and stays there: this is a
// thin re-export, not a second implementation. It exists because everything in
// the v2 stack -- agentinstance.NewService, a2agateway.New, the reconciler --
// takes a store, while api/database only exports the Client *interface*. Without
// a public constructor nothing outside this module can assemble a controller on
// kagent's own building blocks; it can only receive a Client from core/pkg/app.
package database

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	internaldb "github.com/kagent-dev/kagent/go/core/internal/database"
)

// PostgresConfig configures Connect. Aliased rather than redeclared so callers
// and the internal package cannot drift.
type PostgresConfig = internaldb.PostgresConfig

// Connect opens a pgx pool, retrying until the database is reachable or ctx is
// done.
func Connect(ctx context.Context, cfg *PostgresConfig) (*pgxpool.Pool, error) {
	return internaldb.Connect(ctx, cfg)
}

// NewClient returns the Client backed by the given pool.
func NewClient(pool *pgxpool.Pool) dbpkg.Client {
	return internaldb.NewClient(pool)
}

// ResolveURL returns url, or the contents of urlFile when url is empty.
func ResolveURL(url, urlFile string) (string, error) {
	return internaldb.ResolveURL(url, urlFile)
}
