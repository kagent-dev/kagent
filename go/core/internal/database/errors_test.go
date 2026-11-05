package database

import (
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
)

func TestIsUnavailable(t *testing.T) {
	scenarios := []struct {
		name  string
		err   func(t *testing.T) error
		retry bool
	}{
		{"Given PostgreSQL refuses connections, when a login fails, assert that the database is unavailable", func(t *testing.T) error {
			return loginError(t, refusingAddress(t))
		}, true},
		{"Given PostgreSQL is starting up, when a login is rejected with 57P03, assert that the database is unavailable", func(t *testing.T) error {
			return loginError(t, dbtest.RejectLogins(t, "57P03"))
		}, true},
		{"Given a proxy cannot reach PostgreSQL, when a login is rejected with 08001, assert that the database is unavailable", func(t *testing.T) error {
			return loginError(t, dbtest.RejectLogins(t, "08001"))
		}, true},
		{"Given a connect failure, when a caller wraps it, assert that the database is unavailable", func(t *testing.T) error {
			return fmt.Errorf("list sessions: %w", loginError(t, refusingAddress(t)))
		}, true},
		{"Given a wrong password, when a login is rejected with 28P01, assert that the database is not unavailable", func(t *testing.T) error {
			return loginError(t, dbtest.RejectLogins(t, "28P01"))
		}, false},
		{"Given an unknown login, when a login is rejected with 28000, assert that the database is not unavailable", func(t *testing.T) error {
			return loginError(t, dbtest.RejectLogins(t, "28000"))
		}, false},
		{"Given a missing database, when a login is rejected with 3D000, assert that the database is not unavailable", func(t *testing.T) error {
			return loginError(t, dbtest.RejectLogins(t, "3D000"))
		}, false},
		{"Given a login without CONNECT privilege, when a login is rejected with 42501, assert that the database is not unavailable", func(t *testing.T) error {
			return loginError(t, dbtest.RejectLogins(t, "42501"))
		}, false},
		{"Given an open connection, when a statement fails, assert that the database is not unavailable", func(*testing.T) error {
			return &pgconn.PgError{Severity: "ERROR", Code: "23505"}
		}, false},
		{"Given a non-database failure, when it is checked, assert that the database is not unavailable", func(*testing.T) error {
			return errors.New("row decode failed")
		}, false},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			err := scenario.err(t)
			if got := IsUnavailable(err); got != scenario.retry {
				t.Fatalf("IsUnavailable(%v) = %v, want %v", err, got, scenario.retry)
			}
		})
	}
}

func refusingAddress(t *testing.T) string {
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

func loginError(t *testing.T, address string) error {
	t.Helper()
	conn, err := pgconn.Connect(t.Context(), "postgres://kagent@"+address+"/kagent?sslmode=disable")
	if err == nil {
		_ = conn.Close(t.Context())
		t.Fatal("login succeeded")
	}
	var connectErr *pgconn.ConnectError
	if !errors.As(err, &connectErr) {
		t.Fatalf("got %T, want *pgconn.ConnectError", err)
	}
	return err
}
