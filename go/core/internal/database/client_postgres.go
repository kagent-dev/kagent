package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Client persists control-plane state in PostgreSQL. Callers define the narrow
// interfaces they need; SQL rows and protobuf encoding stay inside the store.
type Client struct {
	db *pgxpool.Pool
}

func NewClient(db *pgxpool.Pool) *Client {
	return &Client{db: db}
}

func (c *Client) withTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := c.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// notFoundOr maps the driver's no-rows error to ErrNotFound so callers
// outside this package match on the exported sentinel, never on pgx.
func notFoundOr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func strPtrIfNotEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefStr(s *string) string {
	if s != nil {
		return *s
	}
	return ""
}

func derefInt64(n *int64) int64 {
	if n != nil {
		return *n
	}
	return 0
}

func derefTime(t *time.Time) time.Time {
	if t != nil {
		return *t
	}
	return time.Time{}
}
