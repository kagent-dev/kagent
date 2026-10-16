// Package operations owns external business approvals. It does not restore a
// DSH execution point or own kagent public tasks.
package operations

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

const (
	AwaitingApproval = "awaiting_approval"
	Approved         = "approved"
	Rejected         = "rejected"
	Expired          = "expired"
	OutcomeUnknown   = "outcome_unknown"
	Succeeded        = "succeeded"
	Failed           = "failed"
)

var (
	ErrConflict = errors.New("operation conflicts with its saved request or state")
	ErrNotFound = errors.New("operation not found")
)

// Operation is a durable business action, not an A2A task or native tool call.
type Operation struct {
	ID            string          `json:"operation_id"`
	RequestKey    string          `json:"request_key"`
	RequestDigest string          `json:"request_digest"`
	Binding       string          `json:"binding"`
	Tool          string          `json:"tool"`
	Arguments     json.RawMessage `json:"arguments"`
	State         string          `json:"state"`
	Result        json.RawMessage `json:"result,omitempty"`
	DecidedBy     string          `json:"decided_by,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	ExpiresAt     time.Time       `json:"expires_at"`
	Notification  string          `json:"notification"`
}

// Store uses its own migration ledger and tables; deploy with a dedicated DB.
type Store struct {
	db    *sql.DB
	scope string
}

func OpenStore(ctx context.Context, dsn, scope string) (*Store, error) {
	if scope == "" {
		return nil, errors.New("scope is required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open operation database: %w", err)
	}
	db.SetMaxOpenConns(8)
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockID(741025441))
	if err != nil {
		return nil, errors.Join(err, db.Close())
	}
	files, err := fs.Sub(migrationFiles, "migrations")
	if err == nil {
		var provider *goose.Provider
		provider, err = goose.NewProvider(goose.DialectPostgres, db, files, goose.WithTableName("dsh_approval_schema_migrations"), goose.WithSessionLocker(locker))
		if err == nil {
			_, err = provider.Up(ctx)
		}
	}
	if err != nil {
		return nil, errors.Join(fmt.Errorf("failed to migrate operation database: %w", err), db.Close())
	}
	return &Store{db: db, scope: scope}, nil
}

func (s *Store) Close() error { return s.db.Close() }

const columns = `id::text, request_key, request_digest, binding, tool, arguments, state, result, decided_by, created_at, expires_at, notification`

func scanOperation(row interface{ Scan(...any) error }) (Operation, error) {
	var o Operation
	var arguments, result []byte
	err := row.Scan(&o.ID, &o.RequestKey, &o.RequestDigest, &o.Binding, &o.Tool, &arguments, &o.State, &result, &o.DecidedBy, &o.CreatedAt, &o.ExpiresAt, &o.Notification)
	o.Arguments, o.Result = arguments, result
	if errors.Is(err, sql.ErrNoRows) {
		return o, ErrNotFound
	}
	return o, err
}

func (s *Store) submit(ctx context.Context, o Operation, ttl time.Duration) (Operation, error) {
	// Never overwrite the saved action on a retry, including a different tool or
	// arguments submitted under the same key.
	_, err := s.db.ExecContext(ctx, `INSERT INTO dsh_approval_operations
	(scope,id,request_key,request_digest,binding,tool,arguments,state,expires_at)
	VALUES ($1,$2,$3,$4,$5,$6,$7,'awaiting_approval',now()+$8*interval '1 second')
	ON CONFLICT (scope,request_key) DO NOTHING`, s.scope, uuid.NewString(), o.RequestKey, o.RequestDigest, o.Binding, o.Tool, o.Arguments, ttl.Seconds())
	if err != nil {
		return Operation{}, fmt.Errorf("failed to save operation: %w", err)
	}
	saved, err := scanOperation(s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM dsh_approval_operations WHERE scope=$1 AND request_key=$2`, s.scope, o.RequestKey))
	if err == nil && saved.RequestDigest != o.RequestDigest {
		err = ErrConflict
	}
	return saved, err
}

func (s *Store) Get(ctx context.Context, id string) (Operation, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Operation{}, ErrNotFound
	}
	return scanOperation(s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM dsh_approval_operations WHERE scope=$1 AND id=$2`, s.scope, id))
}

func (s *Store) list(ctx context.Context) ([]Operation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+` FROM dsh_approval_operations WHERE scope=$1 ORDER BY created_at DESC LIMIT 100`, s.scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Operation{}
	for rows.Next() {
		o, err := scanOperation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, o)
	}
	return result, rows.Err()
}

func (s *Store) decide(ctx context.Context, id, digest, decision, actor string) (Operation, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Operation{}, ErrNotFound
	}
	o, err := scanOperation(s.db.QueryRowContext(ctx, `UPDATE dsh_approval_operations
	SET state=$4,decided_by=$5,updated_at=now()
	WHERE scope=$1 AND id=$2 AND request_digest=$3 AND expires_at>now()
	AND (state='awaiting_approval' OR (state='approved' AND $4='rejected')) RETURNING `+columns, s.scope, id, digest, decision, actor))
	if !errors.Is(err, ErrNotFound) {
		return o, err
	}
	o, err = s.Get(ctx, id)
	if err != nil {
		return o, err
	}
	if o.RequestDigest == digest && o.State == decision && o.DecidedBy == actor {
		return o, nil
	}
	return Operation{}, ErrConflict
}

func (s *Store) claim(ctx context.Context, binding string) (Operation, error) {
	// This durable transition precedes ANY remote execution. A crash or lost
	// response leaves outcome_unknown and never automatically repeats effects.
	return scanOperation(s.db.QueryRowContext(ctx, `UPDATE dsh_approval_operations SET state='outcome_unknown',updated_at=now()
	WHERE (scope,id) IN (SELECT scope,id FROM dsh_approval_operations
	WHERE scope=$1 AND binding=$2 AND state='approved' AND expires_at>now()
	ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING `+columns, s.scope, binding))
}

func (s *Store) finish(ctx context.Context, id, state string, result json.RawMessage) error {
	_, err := s.db.ExecContext(ctx, `UPDATE dsh_approval_operations SET state=$3,result=$4,updated_at=now()
	WHERE scope=$1 AND id=$2 AND state='outcome_unknown'`, s.scope, id, state, result)
	return err
}

func (s *Store) expire(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE dsh_approval_operations SET state='expired',updated_at=now()
	WHERE scope=$1 AND state IN ('awaiting_approval','approved') AND expires_at<=now()`, s.scope)
	return err
}

func (s *Store) claimNotification(ctx context.Context, binding string) (Operation, error) {
	return scanOperation(s.db.QueryRowContext(ctx, `UPDATE dsh_approval_operations SET notification='delivery_unknown'
	WHERE (scope,id) IN (SELECT scope,id FROM dsh_approval_operations WHERE scope=$1 AND binding=$2
	AND state IN ('succeeded','failed','rejected','expired') AND notification='pending'
	ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING `+columns, s.scope, binding))
}

func (s *Store) notified(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE dsh_approval_operations SET notification='delivered' WHERE scope=$1 AND id=$2 AND notification='delivery_unknown'`, s.scope, id)
	return err
}

func (s *Store) retryNotification(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE dsh_approval_operations SET notification='pending' WHERE scope=$1 AND id=$2 AND notification='delivery_unknown'`, s.scope, id)
	return err
}
