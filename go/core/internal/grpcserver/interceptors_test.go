package grpcserver

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
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

var testInstanceID = uuid.MustParse("22222222-2222-4222-8222-222222222222")

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
	instanceShare    *dbpkg.AgentInstanceShare
	instanceShareErr error
}

func (s *testShareStore) GetAgentInstanceShareByTokenHash(context.Context, []byte) (*dbpkg.AgentInstanceShare, error) {
	if s.instanceShare == nil && s.instanceShareErr == nil {
		return nil, dbpkg.ErrNotFound
	}
	return s.instanceShare, s.instanceShareErr
}

func TestAuthenticationUnaryInterceptor(t *testing.T) {
	policies := MethodPolicies{
		readMethod:             pkgauth.AccessRead,
		createMethod:           pkgauth.AccessCreate,
		"/test.Service/Public": pkgauth.AccessPublic,
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

	t.Run("an AgentInstance share is attached to a read call", func(t *testing.T) {
		store := &testShareStore{
			instanceShare: &dbpkg.AgentInstanceShare{
				Namespace: "kagent", InstanceID: testInstanceID,
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
				if !share.IsForAgentInstance(testInstanceID.String()) {
					t.Errorf("share is not for instance-1: %#v", share)
				}
				// The owner, not the visitor: the instance read runs as the owner or
				// it finds nothing, because an instance is scoped to its creator.
				if share.UserID != "owner" {
					t.Errorf("UserID = %q, want the owner", share.UserID)
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
			instanceShare: &dbpkg.AgentInstanceShare{
				InstanceID: testInstanceID, Permission: "READ_ONLY", OwnerUserID: "owner",
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
			instanceShare: &dbpkg.AgentInstanceShare{
				InstanceID: testInstanceID, Permission: "READ_WRITE", OwnerUserID: "owner",
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

	t.Run("invalid share token is denied", func(t *testing.T) {
		store := &testShareStore{instanceShareErr: dbpkg.ErrNotFound}
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
