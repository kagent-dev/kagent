package a2a

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	coredatabase "github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
)

func TestSuspendSessionActorOnClose(t *testing.T) {
	t.Parallel()

	var suspended atomic.Bool
	body := &trackingReadCloser{data: []byte("ok")}

	wrapped := &suspendSessionActorOnClose{
		ReadCloser: body,
		suspend: func() {
			suspended.Store(true)
		},
	}

	if _, err := io.ReadAll(wrapped); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if suspended.Load() {
		t.Fatal("suspend should not run before Close")
	}
	if err := wrapped.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !suspended.Load() {
		t.Fatal("expected suspend after Close")
	}
	if body.closed != 1 {
		t.Fatalf("expected underlying body closed once, got %d", body.closed)
	}
}

type trackingReadCloser struct {
	data   []byte
	offset int
	closed int
}

func (t *trackingReadCloser) Read(p []byte) (int, error) {
	if t.offset >= len(t.data) {
		return 0, io.EOF
	}
	n := copy(p, t.data[t.offset:])
	t.offset += n
	return n, nil
}

func (t *trackingReadCloser) Close() error {
	t.closed++
	return nil
}

// TestEnsureSessionRow covers the direct-A2A session-row materialization against a real
// postgres (testcontainer): create the caller's row when missing, leave an existing row
// untouched, and fail without a user identity.
func TestEnsureSessionRow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database test in short mode")
	}
	t.Parallel()

	ctx := context.Background()
	connStr := dbtest.StartT(ctx, t)
	dbtest.MigrateT(t, connStr, false)
	pool, err := coredatabase.Connect(ctx, &coredatabase.PostgresConfig{URL: connStr})
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)
	db := coredatabase.NewClient(pool)

	sa := &v1alpha2.SandboxAgent{
		Spec: v1alpha2.SandboxAgentSpec{
			AgentSpec: v1alpha2.AgentSpec{
				Type:        v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{Runtime: v1alpha2.DeclarativeRuntime_Python},
			},
		},
	}
	sa.Name = "my-agent"
	sa.Namespace = "kagent"
	rt := &substrateSandboxSessionRoundTripper{sandboxAgent: sa, db: db}

	t.Run("creates the row for a new session", func(t *testing.T) {
		if err := rt.ensureSessionRow(ctx, "sess-1", "jm@solo.io"); err != nil {
			t.Fatalf("ensureSessionRow: %v", err)
		}
		row, err := db.GetSession(ctx, "sess-1", "jm@solo.io")
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		if row.AgentID == nil || *row.AgentID != "kagent__NS__my_agent" {
			t.Fatalf("expected session row bound to the agent, got %+v", row)
		}
	})

	t.Run("existing row is left untouched", func(t *testing.T) {
		// A pre-existing row with no agent binding must survive as-is: a second write would
		// have filled AgentID.
		if err := db.StoreSession(ctx, &database.Session{ID: "sess-2", UserID: "jm@solo.io"}); err != nil {
			t.Fatalf("StoreSession: %v", err)
		}
		if err := rt.ensureSessionRow(ctx, "sess-2", "jm@solo.io"); err != nil {
			t.Fatalf("ensureSessionRow: %v", err)
		}
		row, err := db.GetSession(ctx, "sess-2", "jm@solo.io")
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		if row.AgentID != nil {
			t.Fatalf("expected the existing row untouched, got AgentID %q", *row.AgentID)
		}
	})

	t.Run("missing user identity fails without storing", func(t *testing.T) {
		if err := rt.ensureSessionRow(ctx, "sess-3", ""); err == nil {
			t.Fatal("expected an error when the request carries no user identity")
		}
		if _, err := db.GetSession(ctx, "sess-3", ""); !errors.Is(err, database.ErrNotFound) {
			t.Fatalf("expected no row, got %v", err)
		}
	})
}

func TestExtractA2AContextID(t *testing.T) {
	t.Parallel()

	got, err := extractA2AContextID([]byte(`{"params":{"message":{"contextId":"sess-1"}}}`))
	if err != nil {
		t.Fatalf("extractA2AContextID: %v", err)
	}
	if got != "sess-1" {
		t.Fatalf("got %q, want sess-1", got)
	}
}

func TestSuspendSessionActorOnClosePreservesBody(t *testing.T) {
	t.Parallel()

	rc := &suspendSessionActorOnClose{
		ReadCloser: io.NopCloser(strings.NewReader("payload")),
		suspend:    func() {},
	}
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "payload" {
		t.Fatalf("got %q", got)
	}
}
