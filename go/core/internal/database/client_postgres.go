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
	db                   retryingPool
	vectorCosineOperator string
}

// ClientOption configures a Client built by NewClient.
type ClientOption func(*Client)

// WithConnectRetry makes each store call retry a failed PostgreSQL login for up
// to timeout. A call can take up to timeout plus one attempt. Rejected
// credentials are not retried. Zero makes one attempt.
func WithConnectRetry(timeout time.Duration) ClientOption {
	return func(c *Client) { c.db.retryFor = timeout }
}

// NewClient wraps an existing PostgreSQL pool without connecting or migrating. The caller
// owns the pool and must close it. vectorSchema is the schema pgvector is installed in.
func NewClient(db *pgxpool.Pool, vectorSchema string, opts ...ClientOption) *Client {
	c := &Client{db: retryingPool{pool: db}, vectorCosineOperator: "OPERATOR(" + pgx.Identifier{vectorSchema}.Sanitize() + ".<=>)"}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// withTx commits all callback writes together on success and rolls them back on failure,
// returning callback or transaction errors. The callback must keep external network work
// outside the transaction.
func (c *Client) withTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := c.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// notFoundOr maps pgx.ErrNoRows to ErrNotFound and leaves all other errors, including nil,
// unchanged.
func notFoundOr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// derefStr returns the pointed-to string, or an empty string for nil.
func derefStr(s *string) string {
	if s != nil {
		return *s
	}
	return ""
}
