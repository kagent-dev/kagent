package a2a

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
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

// fakeSessionDB stubs the two database.Client methods ensureSessionRow uses; the embedded nil
// interface panics on anything else.
type fakeSessionDB struct {
	database.Client
	sessions map[string]*database.Session
	stored   int
}

func sessionKey(id, userID string) string { return id + "\x00" + userID }

func (f *fakeSessionDB) GetSession(_ context.Context, sessionID, userID string) (*database.Session, error) {
	if s, ok := f.sessions[sessionKey(sessionID, userID)]; ok {
		return s, nil
	}
	return nil, fmt.Errorf("failed to get session %s: %w", sessionID, pgx.ErrNoRows)
}

func (f *fakeSessionDB) StoreSession(_ context.Context, s *database.Session) error {
	f.sessions[sessionKey(s.ID, s.UserID)] = s
	f.stored++
	return nil
}

// TestEnsureSessionRow covers the direct-A2A session-row materialization: create the caller's
// row when missing, no write when it already exists, fail without a user identity, and no-op
// for agents that keep sessions in the controller database (BYO without the opt-in annotation).
func TestEnsureSessionRow(t *testing.T) {
	t.Parallel()

	declarativeAgent := func() *v1alpha2.SandboxAgent {
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
		return sa
	}

	t.Run("creates the row for a new session", func(t *testing.T) {
		t.Parallel()
		db := &fakeSessionDB{sessions: map[string]*database.Session{}}
		rt := &substrateSandboxSessionRoundTripper{sandboxAgent: declarativeAgent(), db: db}

		if err := rt.ensureSessionRow(context.Background(), "sess-1", "jm@solo.io"); err != nil {
			t.Fatalf("ensureSessionRow: %v", err)
		}
		row := db.sessions[sessionKey("sess-1", "jm@solo.io")]
		if row == nil || row.AgentID == nil || *row.AgentID != "kagent__NS__my_agent" {
			t.Fatalf("expected session row bound to the agent, got %+v", row)
		}
	})

	t.Run("existing row is left untouched", func(t *testing.T) {
		t.Parallel()
		db := &fakeSessionDB{sessions: map[string]*database.Session{
			sessionKey("sess-1", "jm@solo.io"): {ID: "sess-1", UserID: "jm@solo.io"},
		}}
		rt := &substrateSandboxSessionRoundTripper{sandboxAgent: declarativeAgent(), db: db}

		if err := rt.ensureSessionRow(context.Background(), "sess-1", "jm@solo.io"); err != nil {
			t.Fatalf("ensureSessionRow: %v", err)
		}
		if db.stored != 0 {
			t.Fatalf("expected no store for an existing row, got %d", db.stored)
		}
	})

	t.Run("missing user identity fails without storing", func(t *testing.T) {
		t.Parallel()
		db := &fakeSessionDB{sessions: map[string]*database.Session{}}
		rt := &substrateSandboxSessionRoundTripper{sandboxAgent: declarativeAgent(), db: db}

		if err := rt.ensureSessionRow(context.Background(), "sess-1", ""); err == nil {
			t.Fatal("expected an error when the request carries no user identity")
		}
		if db.stored != 0 {
			t.Fatalf("expected no store, got %d", db.stored)
		}
	})

	t.Run("no-op for BYO agents on controller sessions", func(t *testing.T) {
		t.Parallel()
		sa := declarativeAgent()
		sa.Spec.AgentSpec = v1alpha2.AgentSpec{Type: v1alpha2.AgentType_BYO, BYO: &v1alpha2.BYOAgentSpec{}}
		db := &fakeSessionDB{sessions: map[string]*database.Session{}}
		rt := &substrateSandboxSessionRoundTripper{sandboxAgent: sa, db: db}

		if err := rt.ensureSessionRow(context.Background(), "sess-1", "jm@solo.io"); err != nil {
			t.Fatalf("ensureSessionRow: %v", err)
		}
		if db.stored != 0 {
			t.Fatalf("expected no store for a controller-session agent, got %d", db.stored)
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
