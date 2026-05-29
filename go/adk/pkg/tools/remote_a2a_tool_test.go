package tools

import (
	"context"
	"testing"

	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2asrv"
)

// newReq returns an empty outbound client Request with an initialized CallMeta.
func newReq() *a2aclient.Request {
	return &a2aclient.Request{Meta: a2aclient.CallMeta{}}
}

// withCallContext returns a context that carries an a2asrv.CallContext whose
// RequestMeta exposes the given inbound headers, so the interceptor's
// CallContextFrom + RequestMeta path can be exercised.
func withCallContext(parent context.Context, inbound map[string][]string) context.Context {
	ctx, _ := a2asrv.WithCallContext(parent, a2asrv.NewRequestMeta(inbound))
	return ctx
}

// TestLineageHeaderPropagation covers the parent + root context_id header
// derivation. Mirrors the Python TestLineageHeaderPropagation cases in
// python/packages/kagent-adk/tests/unittests/test_remote_a2a_tool.py.
func TestLineageHeaderPropagation(t *testing.T) {
	const ownSession = "own-session-123"
	const upstreamRoot = "root-from-upstream"
	const upstreamParent = "parent-from-upstream"

	t.Run("chain root stamps own id as parent and root", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), parentContextIDContextKey{}, ownSession)
		req := newReq()

		if _, err := (&lineageHeadersInterceptor{}).Before(ctx, req); err != nil {
			t.Fatalf("Before returned error: %v", err)
		}

		assertSingleHeader(t, req, ParentContextIDHeader, ownSession)
		assertSingleHeader(t, req, RootContextIDHeader, ownSession)
	})

	t.Run("mid-chain forwards root unchanged and overrides parent with own id", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), parentContextIDContextKey{}, ownSession)
		ctx = withCallContext(ctx, map[string][]string{
			RootContextIDHeader:   {upstreamRoot},
			ParentContextIDHeader: {upstreamParent},
		})
		req := newReq()

		if _, err := (&lineageHeadersInterceptor{}).Before(ctx, req); err != nil {
			t.Fatalf("Before returned error: %v", err)
		}

		assertSingleHeader(t, req, ParentContextIDHeader, ownSession)
		assertSingleHeader(t, req, RootContextIDHeader, upstreamRoot)
	})

	t.Run("inbound parent header alone does not seed root", func(t *testing.T) {
		// Both lineage headers are introduced together, so an inbound request
		// carrying only a parent header is not a real upstream root. Root must
		// fall back to our own session id, not the inbound parent.
		ctx := context.WithValue(context.Background(), parentContextIDContextKey{}, ownSession)
		ctx = withCallContext(ctx, map[string][]string{
			ParentContextIDHeader: {upstreamParent},
		})
		req := newReq()

		if _, err := (&lineageHeadersInterceptor{}).Before(ctx, req); err != nil {
			t.Fatalf("Before returned error: %v", err)
		}

		assertSingleHeader(t, req, ParentContextIDHeader, ownSession)
		assertSingleHeader(t, req, RootContextIDHeader, ownSession)
	})

	t.Run("no session id is a no-op", func(t *testing.T) {
		// No parentContextIDContextKey on ctx - matches the stub tool_context
		// case in Python (empty dict, no headers stamped).
		ctx := context.Background()
		req := newReq()

		if _, err := (&lineageHeadersInterceptor{}).Before(ctx, req); err != nil {
			t.Fatalf("Before returned error: %v", err)
		}

		if got := req.Meta.Get(ParentContextIDHeader); len(got) != 0 {
			t.Errorf("expected no parent header, got %v", got)
		}
		if got := req.Meta.Get(RootContextIDHeader); len(got) != 0 {
			t.Errorf("expected no root header, got %v", got)
		}
	})

	t.Run("pre-existing header on req.Meta wins over lineage", func(t *testing.T) {
		// Analogous to Python's header_provider override: a caller-supplied
		// header that is already present on the outbound request must not be
		// overwritten by the lineage interceptor.
		ctx := context.WithValue(context.Background(), parentContextIDContextKey{}, ownSession)
		ctx = withCallContext(ctx, map[string][]string{
			RootContextIDHeader: {upstreamRoot},
		})
		req := newReq()
		req.Meta.Append(ParentContextIDHeader, "caller-override-parent")
		req.Meta.Append(RootContextIDHeader, "caller-override-root")

		if _, err := (&lineageHeadersInterceptor{}).Before(ctx, req); err != nil {
			t.Fatalf("Before returned error: %v", err)
		}

		assertSingleHeader(t, req, ParentContextIDHeader, "caller-override-parent")
		assertSingleHeader(t, req, RootContextIDHeader, "caller-override-root")
	})
}

func assertSingleHeader(t *testing.T, req *a2aclient.Request, key, want string) {
	t.Helper()
	got := req.Meta.Get(key)
	if len(got) != 1 {
		t.Fatalf("%s: expected exactly 1 value, got %v", key, got)
	}
	if got[0] != want {
		t.Errorf("%s: got %q, want %q", key, got[0], want)
	}
}
