// Package dbtest provides PostgreSQL test helpers: a real Postgres container and
// a fake server that rejects logins.
package dbtest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgxpool"
	testcontainers "github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
	"github.com/kagent-dev/kagent/go/core/pkg/postgres"
)

// Role is the test database's login user. Test connections assume it as their
// role, which PostgreSQL allows for any login.
const Role = "postgres"

// Start starts a pgvector Postgres container and returns the connection string
// and a cleanup function. Callers are responsible for calling cleanup when done.
func Start(ctx context.Context) (connStr string, cleanup func(), err error) {
	pgContainer, err := tcpostgres.Run(ctx,
		"pgvector/pgvector:pg18-trixie",
		tcpostgres.WithDatabase("kagent_test"),
		tcpostgres.WithUsername(Role),
		tcpostgres.WithPassword("kagent"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		// Run returns the created container when it fails to start.
		return "", nil, errors.Join(fmt.Errorf("starting postgres container: %w", err), testcontainers.TerminateContainer(pgContainer))
	}

	connStr, err = pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return "", nil, errors.Join(fmt.Errorf("getting connection string: %w", err), testcontainers.TerminateContainer(pgContainer))
	}

	cleanup = func() {
		// Not ctx: a test context is canceled before cleanup runs.
		if err := testcontainers.TerminateContainer(pgContainer); err != nil {
			fmt.Printf("warning: failed to terminate postgres container: %v\n", err)
		}
	}

	return connStr, cleanup, nil
}

// StartT starts a pgvector Postgres container and registers cleanup with t.Cleanup.
// Suitable for use in individual tests or test helpers that have a *testing.T.
func StartT(ctx context.Context, t *testing.T) string {
	t.Helper()

	connStr, cleanup, err := Start(ctx)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}
	t.Cleanup(cleanup)

	return connStr
}

// CloneT creates an isolated database from a template prepared using Start and
// Migrate. Close all connections to the template before cloning it. Connections
// to the clone must close before its registered cleanup runs.
func CloneT(t *testing.T, templateConnStr string) string {
	t.Helper()
	connURL, err := url.Parse(templateConnStr)
	if err != nil {
		t.Fatalf("parse template connection string: %v", err)
	}
	templateName := strings.TrimPrefix(connURL.Path, "/")
	connURL.Path = "/postgres"
	admin, err := pgx.Connect(t.Context(), connURL.String())
	if err != nil {
		t.Fatalf("connect to PostgreSQL for database cloning: %v", err)
	}
	t.Cleanup(func() {
		if err := admin.Close(context.Background()); err != nil {
			t.Errorf("close database cloning connection: %v", err)
		}
	})

	name := "test_" + uuid.NewString()
	quotedName := pgx.Identifier{name}.Sanitize()
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+quotedName+" TEMPLATE "+pgx.Identifier{templateName}.Sanitize())
	if err != nil {
		t.Fatalf("clone test database: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP DATABASE "+quotedName); err != nil {
			t.Errorf("drop cloned test database: %v", err)
		}
	})
	connURL.Path = "/" + name
	return connURL.String()
}

// Migrate installs pgvector for the test database when enabled, then runs the
// embedded migrations against connStr.
// Use MigrateT in tests that have a *testing.T; use Migrate in TestMain where no T is available.
func Migrate(connStr string, vectorEnabled bool) error {
	if vectorEnabled {
		db, err := sql.Open("pgx", connStr)
		if err != nil {
			return err
		}
		defer db.Close()
		if _, err := db.Exec("CREATE EXTENSION IF NOT EXISTS vector WITH SCHEMA public"); err != nil {
			return err
		}
	}
	return migrations.RunUp(context.Background(), PoolConfig(connStr), migrations.BuiltinSourcesInSchema(vectorEnabled, "public", "public"))
}

// PoolConfig returns the pool configuration for connStr that assumes Role. It
// panics on a malformed connection string, which only a broken test can produce.
func PoolConfig(connStr string) *pgxpool.Config {
	config, err := postgres.PoolConfig(connStr, Role)
	if err != nil {
		panic(err)
	}
	return config
}

// RefuseConnections returns a local address with no listener, so a dial to it
// is refused.
func RefuseConnections(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

// LoginError logs in to address and returns the *pgconn.ConnectError it fails with.
func LoginError(t *testing.T, ctx context.Context, address string) error {
	t.Helper()
	conn, err := pgconn.Connect(ctx, "postgres://kagent@"+address+"/kagent?sslmode=disable")
	if err == nil {
		_ = conn.Close(ctx)
		t.Fatal("login succeeded")
	}
	var connectErr *pgconn.ConnectError
	if !errors.As(err, &connectErr) {
		t.Fatalf("got %T, want *pgconn.ConnectError", err)
	}
	return err
}

// RejectLogins starts a server that answers every login with a FATAL error
// carrying sqlstate, such as 28P01. It stops when t ends.
func RejectLogins(t *testing.T, sqlstate string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				backend := pgproto3.NewBackend(conn, conn)
				if _, err := backend.ReceiveStartupMessage(); err != nil {
					return
				}
				backend.Send(&pgproto3.ErrorResponse{Severity: "FATAL", Code: sqlstate, Message: "login rejected"})
				_ = backend.Flush()
			}()
		}
	}()
	return listener.Addr().String()
}

// MigrateT runs the embedded migrations against connStr and calls t.Fatal on error.
// If vectorEnabled is true the vector pass is also applied.
func MigrateT(t *testing.T, connStr string, vectorEnabled bool) {
	t.Helper()
	if err := Migrate(connStr, vectorEnabled); err != nil {
		t.Fatalf("dbtest.MigrateT: %v", err)
	}
}
