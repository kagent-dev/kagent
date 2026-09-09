package database

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// dbExecutor lets the same store operation use a pool or its caller's transaction.
type dbExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

var (
	_ dbExecutor = (*pgxpool.Pool)(nil)
	_ dbExecutor = (pgx.Tx)(nil)
)

// queryOne runs parameterized SQL through the supplied pool or transaction and maps its
// first row. It returns pgx.ErrNoRows for no results and propagates query/scan errors; it
// closes rows and does not enforce uniqueness.
func queryOne[T any](ctx context.Context, db dbExecutor, sql string, scan pgx.RowToFunc[T], args ...any) (T, error) {
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		var zero T
		return zero, err
	}
	return pgx.CollectOneRow(rows, scan)
}

// queryMany runs parameterized SQL through the supplied pool or transaction and maps all
// rows in query order. It closes rows and propagates query, scan, and iteration errors.
func queryMany[T any](ctx context.Context, db dbExecutor, sql string, scan pgx.RowToFunc[T], args ...any) ([]T, error) {
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scan)
}

// execSQL executes parameterized SQL through the supplied pool or transaction and discards
// the affected-row count. Matching no rows is not an error.
func execSQL(ctx context.Context, db dbExecutor, sql string, args ...any) error {
	_, err := db.Exec(ctx, sql, args...)
	return err
}
