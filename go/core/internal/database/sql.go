package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// dbExecutor lets the same store operation use a pool or its caller's transaction.
type dbExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

var (
	_ dbExecutor = retryingPool{}
	_ dbExecutor = (pgx.Tx)(nil)
)

// retryingPool retries failed logins on every store call. A login fails before
// any SQL is sent, so a retry never repeats a statement.
type retryingPool struct {
	pool *pgxpool.Pool
	// retryFor bounds when a new attempt may start. Zero makes one attempt.
	retryFor time.Duration
}

func (p retryingPool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return retryLogin(ctx, p.retryFor, func() (pgconn.CommandTag, error) { return p.pool.Exec(ctx, sql, args...) })
}

func (p retryingPool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return retryLogin(ctx, p.retryFor, func() (pgx.Rows, error) { return p.pool.Query(ctx, sql, args...) })
}

// BeginTx also lets pgx.BeginTxFunc take the pool.
func (p retryingPool) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	return retryLogin(ctx, p.retryFor, func() (pgx.Tx, error) { return p.pool.BeginTx(ctx, opts) })
}

// retryLogin calls op until it succeeds, fails for another reason, ctx ends, or
// retryFor has passed. op runs on ctx, so a running statement is never cut.
func retryLogin[T any](ctx context.Context, retryFor time.Duration, op func() (T, error)) (T, error) {
	if retryFor <= 0 {
		return op()
	}
	start := time.Now()
	delay := defaultInitialDelay
	for attempt := 1; ; attempt++ {
		result, err := op()
		if err == nil || !IsUnavailable(err) {
			return result, err
		}
		if ctx.Err() != nil {
			return result, errors.Join(err, ctx.Err())
		}
		remaining := retryFor - time.Since(start)
		if remaining <= 0 {
			return result, fmt.Errorf("database unavailable after %d login attempts in %s: %w", attempt, time.Since(start).Round(time.Millisecond), err)
		}
		logging.FromContext(ctx).WarnContext(ctx, "database login failed, retrying", "error", err, "attempt", attempt, "elapsed_ms", time.Since(start).Milliseconds())
		select {
		case <-ctx.Done():
			return result, errors.Join(err, ctx.Err())
		case <-time.After(min(delay, remaining)):
		}
		delay = min(2*delay, defaultMaxDelay)
	}
}

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
