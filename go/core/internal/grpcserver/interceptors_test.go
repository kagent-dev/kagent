package grpcserver

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	httperrors "github.com/kagent-dev/kagent/go/core/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	readMethod   = "/test.Service/Get"
	createMethod = "/test.Service/Create"
)

type testSession struct {
	principal pkgauth.Principal
}

func (s *testSession) Principal() pkgauth.Principal {
	return s.principal
}

type testAuthenticator struct {
	session pkgauth.Session
	err     error
	headers http.Header
}

func (a *testAuthenticator) Authenticate(_ context.Context, headers http.Header, _ url.Values) (pkgauth.Session, error) {
	a.headers = headers.Clone()
	return a.session, a.err
}

func (*testAuthenticator) UpstreamAuth(*http.Request, pkgauth.Session, pkgauth.Principal) error {
	return nil
}

type testShareStore struct {
	share          *dbpkg.SessionShare
	err            error
	recordedUserID string
	recordedShare  int64

	// The AgentInstance half. A share link carries one token and the reader cannot
	// know which kind it is, so the interceptor tries both stores.
	instanceShare    *dbpkg.AgentInstanceShare
	instanceShareErr error
}

func (s *testShareStore) GetSessionShareByToken(context.Context, string) (*dbpkg.SessionShare, error) {
	return s.share, s.err
}

func (s *testShareStore) GetAgentInstanceShareByTokenHash(context.Context, []byte) (*dbpkg.AgentInstanceShare, error) {
	if s.instanceShare == nil && s.instanceShareErr == nil {
		return nil, dbpkg.ErrNotFound
	}
	return s.instanceShare, s.instanceShareErr
}

func (s *testShareStore) RecordShareAccess(_ context.Context, userID string, shareID int64) error {
	s.recordedUserID = userID
	s.recordedShare = shareID
	return nil
}

func TestAuthenticationUnaryInterceptor(t *testing.T) {
	policies := MethodPolicies{
		readMethod:             AccessRead,
		createMethod:           AccessCreate,
		"/test.Service/Public": AccessPublic,
	}
	session := &testSession{principal: pkgauth.Principal{User: pkgauth.User{ID: "caller"}}}

	t.Run("public method bypasses authentication", func(t *testing.T) {
		called := false
		_, err := authenticationUnaryInterceptor(nil, nil, policies)(
			t.Context(), nil, &grpc.UnaryServerInfo{FullMethod: "/test.Service/Public"},
			func(context.Context, any) (any, error) {
				called = true
				return nil, nil
			},
		)
		if err != nil || !called {
			t.Fatalf("public call error = %v, called = %v", err, called)
		}
	})

	t.Run("unconfigured policy is denied", func(t *testing.T) {
		_, err := authenticationUnaryInterceptor(nil, nil, policies)(
			t.Context(), nil, &grpc.UnaryServerInfo{FullMethod: "/test.Service/Missing"},
			func(context.Context, any) (any, error) { return nil, nil },
		)
		if got := status.Code(err); got != codes.PermissionDenied {
			t.Fatalf("code = %v, want PermissionDenied", got)
		}
	})

	t.Run("approved metadata reaches authenticator and context", func(t *testing.T) {
		authenticator := &testAuthenticator{session: session}
		ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(
			"authorization", "Bearer token",
			"x-user-id", "caller",
			"x-agent-name", "default/agent",
			"x-unapproved", "do-not-forward",
		))
		_, err := authenticationUnaryInterceptor(authenticator, nil, policies)(
			ctx, nil, &grpc.UnaryServerInfo{FullMethod: readMethod},
			func(ctx context.Context, _ any) (any, error) {
				gotSession, ok := pkgauth.AuthSessionFrom(ctx)
				if !ok || gotSession.Principal().User.ID != "caller" {
					t.Fatalf("authenticated session = %#v, %v", gotSession, ok)
				}
				return nil, nil
			},
		)
		if err != nil {
			t.Fatalf("interceptor error = %v", err)
		}
		if got := authenticator.headers.Get("Authorization"); got != "Bearer token" {
			t.Errorf("Authorization = %q", got)
		}
		if got := authenticator.headers.Get("X-Agent-Name"); got != "default/agent" {
			t.Errorf("X-Agent-Name = %q", got)
		}
		if got := authenticator.headers.Get("X-Unapproved"); got != "" {
			t.Errorf("X-Unapproved = %q, want empty", got)
		}
	})

	t.Run("read-only share is attached to read call", func(t *testing.T) {
		authenticator := &testAuthenticator{session: session}
		store := &testShareStore{share: &dbpkg.SessionShare{
			ID: 42, Token: "share", SessionID: "session-1", UserID: "owner", ReadOnly: true,
		}}
		ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("x-share-token", "share"))
		_, err := authenticationUnaryInterceptor(authenticator, store, policies)(
			ctx, nil, &grpc.UnaryServerInfo{FullMethod: readMethod},
			func(ctx context.Context, _ any) (any, error) {
				share, ok := pkgauth.ShareContextFrom(ctx)
				if !ok || share.SessionID != "session-1" || share.UserID != "owner" || !share.ReadOnly {
					t.Fatalf("share context = %#v, %v", share, ok)
				}
				return nil, nil
			},
		)
		if err != nil {
			t.Fatalf("interceptor error = %v", err)
		}
		if store.recordedUserID != "caller" || store.recordedShare != 42 {
			t.Fatalf("recorded access = %q, %d", store.recordedUserID, store.recordedShare)
		}
	})

	t.Run("read-only share cannot mutate", func(t *testing.T) {
		store := &testShareStore{share: &dbpkg.SessionShare{ReadOnly: true}}
		ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("x-share-token", "share"))
		_, err := authenticationUnaryInterceptor(&testAuthenticator{session: session}, store, policies)(
			ctx, nil, &grpc.UnaryServerInfo{FullMethod: createMethod},
			func(context.Context, any) (any, error) {
				t.Fatal("handler should not run")
				return nil, nil
			},
		)
		if got := status.Code(err); got != codes.PermissionDenied {
			t.Fatalf("code = %v, want PermissionDenied", got)
		}
	})

	/*
	 * AgentInstance shares. A share link carries one token and the reader opening it
	 * cannot know which kind it is, so the interceptor tries the session store and
	 * then the instance store — and the resulting context names exactly one of the
	 * two resources, so nothing downstream can mistake one for the other.
	 */
	t.Run("an AgentInstance share is resolved when no session share matches", func(t *testing.T) {
		store := &testShareStore{
			// No session share by this token.
			err: dbpkg.ErrNotFound,
			instanceShare: &dbpkg.AgentInstanceShare{
				ID: "share-1", Namespace: "kagent", InstanceID: "instance-1",
				Permission: "READ_ONLY", OwnerUserID: "owner",
			},
		}
		ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("x-share-token", "share"))
		_, err := authenticationUnaryInterceptor(&testAuthenticator{session: session}, store, policies)(
			ctx, nil, &grpc.UnaryServerInfo{FullMethod: readMethod},
			func(ctx context.Context, _ any) (any, error) {
				share, ok := pkgauth.ShareContextFrom(ctx)
				if !ok {
					t.Fatal("no share context")
				}
				if !share.IsForAgentInstance("instance-1") {
					t.Errorf("share is not for instance-1: %#v", share)
				}
				// The owner, not the visitor: the instance read runs as the owner or
				// it finds nothing, because an instance is scoped to its creator.
				if share.UserID != "owner" {
					t.Errorf("UserID = %q, want the owner", share.UserID)
				}
				// A session share and an instance share must never be confusable.
				if share.SessionID != "" {
					t.Errorf("SessionID = %q, want empty on an instance share", share.SessionID)
				}
				if !share.ReadOnly {
					t.Error("READ_ONLY should be read-only")
				}
				return nil, nil
			},
		)
		if err != nil {
			t.Fatalf("interceptor error = %v", err)
		}
	})

	t.Run("a read-only AgentInstance share cannot send", func(t *testing.T) {
		store := &testShareStore{
			err: dbpkg.ErrNotFound,
			instanceShare: &dbpkg.AgentInstanceShare{
				InstanceID: "instance-1", Permission: "READ_ONLY", OwnerUserID: "owner",
			},
		}
		ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("x-share-token", "share"))
		_, err := authenticationUnaryInterceptor(&testAuthenticator{session: session}, store, policies)(
			ctx, nil, &grpc.UnaryServerInfo{FullMethod: createMethod},
			func(context.Context, any) (any, error) {
				t.Fatal("handler should not run")
				return nil, nil
			},
		)
		if got := status.Code(err); got != codes.PermissionDenied {
			t.Fatalf("code = %v, want PermissionDenied", got)
		}
	})

	t.Run("a READ_WRITE AgentInstance share may send", func(t *testing.T) {
		store := &testShareStore{
			err: dbpkg.ErrNotFound,
			instanceShare: &dbpkg.AgentInstanceShare{
				InstanceID: "instance-1", Permission: "READ_WRITE", OwnerUserID: "owner",
			},
		}
		ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("x-share-token", "share"))
		ran := false
		_, err := authenticationUnaryInterceptor(&testAuthenticator{session: session}, store, policies)(
			ctx, nil, &grpc.UnaryServerInfo{FullMethod: createMethod},
			func(ctx context.Context, _ any) (any, error) {
				ran = true
				share, _ := pkgauth.ShareContextFrom(ctx)
				if share.ReadOnly {
					t.Error("READ_WRITE should not be read-only")
				}
				return nil, nil
			},
		)
		if err != nil {
			t.Fatalf("interceptor error = %v", err)
		}
		if !ran {
			t.Fatal("handler did not run")
		}
	})

	t.Run("a session share is not authority over an instance", func(t *testing.T) {
		// The whole reason the two ids are separate fields. A session share reaching
		// the A2A gateway must not be treated as authority over an instance whose id
		// happens to match.
		store := &testShareStore{share: &dbpkg.SessionShare{
			ID: 7, Token: "share", SessionID: "instance-1", UserID: "owner", ReadOnly: true,
		}}
		ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("x-share-token", "share"))
		_, err := authenticationUnaryInterceptor(&testAuthenticator{session: session}, store, policies)(
			ctx, nil, &grpc.UnaryServerInfo{FullMethod: readMethod},
			func(ctx context.Context, _ any) (any, error) {
				share, _ := pkgauth.ShareContextFrom(ctx)
				if share.IsForAgentInstance("instance-1") {
					t.Error("a session share must not read as authority over an instance")
				}
				return nil, nil
			},
		)
		if err != nil {
			t.Fatalf("interceptor error = %v", err)
		}
	})

	t.Run("invalid share token is denied", func(t *testing.T) {
		store := &testShareStore{err: dbpkg.ErrNotFound}
		ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("x-share-token", "missing"))
		_, err := authenticationUnaryInterceptor(&testAuthenticator{session: session}, store, policies)(
			ctx, nil, &grpc.UnaryServerInfo{FullMethod: readMethod},
			func(context.Context, any) (any, error) { return nil, nil },
		)
		if got := status.Code(err); got != codes.PermissionDenied {
			t.Fatalf("code = %v, want PermissionDenied", got)
		}
	})
}

func TestMapError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want codes.Code
	}{
		{"canceled", context.Canceled, codes.Canceled},
		{"deadline", context.DeadlineExceeded, codes.DeadlineExceeded},
		{"bad request", httperrors.NewBadRequestError("bad", nil), codes.InvalidArgument},
		{"not found", httperrors.NewNotFoundError("missing", nil), codes.NotFound},
		{"conflict", httperrors.NewConflictError("conflict", nil), codes.Aborted},
		{"forbidden", httperrors.NewForbiddenError("forbidden", nil), codes.PermissionDenied},
		{"service invalid argument", serviceerrors.NewInvalidArgument("invalid", nil), codes.InvalidArgument},
		{"service unauthenticated", serviceerrors.NewUnauthenticated("unauthenticated", nil), codes.Unauthenticated},
		{"service permission denied", serviceerrors.NewPermissionDenied("denied", nil), codes.PermissionDenied},
		{"service not found", serviceerrors.NewNotFound("missing", nil), codes.NotFound},
		{"service already exists", serviceerrors.NewAlreadyExists("exists", nil), codes.AlreadyExists},
		{"service failed precondition", serviceerrors.NewFailedPrecondition("precondition", nil), codes.FailedPrecondition},
		{"service resource exhausted", serviceerrors.NewResourceExhausted("exhausted", nil), codes.ResourceExhausted},
		{"service aborted", serviceerrors.NewAborted("aborted", nil), codes.Aborted},
		{"service unavailable", serviceerrors.NewUnavailable("unavailable", nil), codes.Unavailable},
		{"service internal", serviceerrors.NewInternal("internal detail", nil), codes.Internal},
		{"unknown redacted", errors.New("database secret"), codes.Internal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mapped := mapError(test.err)
			if got := status.Code(mapped); got != test.want {
				t.Fatalf("code = %v, want %v", got, test.want)
			}
			if (test.name == "unknown redacted" || test.name == "service internal") && status.Convert(mapped).Message() != "internal server error" {
				t.Fatalf("message = %q", status.Convert(mapped).Message())
			}
		})
	}
}

func TestRecoverUnaryInterceptor(t *testing.T) {
	_, err := recoverUnaryInterceptor(t.Context(), nil, &grpc.UnaryServerInfo{FullMethod: readMethod}, func(context.Context, any) (any, error) {
		panic("sensitive panic detail")
	})
	if got := status.Code(err); got != codes.Internal {
		t.Fatalf("code = %v, want Internal", got)
	}
	if got := status.Convert(err).Message(); got != "internal server error" {
		t.Fatalf("message = %q", got)
	}
}

func TestServerMetricsUnaryInterceptor(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := newServerMetrics(registry)
	if err != nil {
		t.Fatalf("newServerMetrics() error = %v", err)
	}
	_, callErr := metrics.unaryInterceptor(t.Context(), nil, &grpc.UnaryServerInfo{FullMethod: readMethod}, func(context.Context, any) (any, error) {
		return nil, status.Error(codes.NotFound, "missing")
	})
	if status.Code(callErr) != codes.NotFound {
		t.Fatalf("call code = %v", status.Code(callErr))
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("registry.Gather() error = %v", err)
	}
	for _, family := range families {
		if family.GetName() == "kagent_grpc_server_requests_total" {
			if got := family.GetMetric()[0].GetCounter().GetValue(); got != 1 {
				t.Fatalf("request counter = %v, want 1", got)
			}
			return
		}
	}
	t.Fatal("request counter metric was not gathered")
}
