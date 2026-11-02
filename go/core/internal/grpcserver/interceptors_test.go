package grpcserver

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
)

const (
	readMethod   = "/test.Service/Get"
	createMethod = "/test.Service/Create"
)

var testSessionID = uuid.MustParse("22222222-2222-4222-8222-222222222222")

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

func (*testAuthenticator) UpstreamAuth(*http.Request, pkgauth.Session, string) error {
	return nil
}

type testShareStore struct {
	sessionShare    *apiv1.SessionShare
	sessionShareErr error
	ownerUserID     string
}

func (s *testShareStore) GetSessionShareByTokenHash(context.Context, []byte) (*apiv1.SessionShare, string, error) {
	if s.sessionShare == nil && s.sessionShareErr == nil {
		return nil, "", database.ErrNotFound
	}
	return s.sessionShare, s.ownerUserID, s.sessionShareErr
}

func TestAuthenticationUnaryInterceptor(t *testing.T) {
	policies := MethodPolicies{
		readMethod:             pkgauth.AccessRead,
		createMethod:           pkgauth.AccessCreate,
		"/test.Service/Public": pkgauth.AccessPublic,
	}
	session := &testSession{principal: pkgauth.Principal{ID: "caller"}}

	t.Run("public method bypasses authentication", func(t *testing.T) {
		called := false
		_, err := authenticationUnaryInterceptor(nil, nil, policies, nil)(
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
		_, err := authenticationUnaryInterceptor(nil, nil, policies, nil)(
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
		_, err := authenticationUnaryInterceptor(authenticator, nil, policies, nil)(
			ctx, nil, &grpc.UnaryServerInfo{FullMethod: readMethod},
			func(ctx context.Context, _ any) (any, error) {
				gotSession, ok := pkgauth.AuthSessionFrom(ctx)
				if !ok || gotSession.Principal().ID != "caller" {
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

	t.Run("a Session share is attached to a read call", func(t *testing.T) {
		store := &testShareStore{
			sessionShare: &apiv1.SessionShare{SessionId: testSessionID.String()}, ownerUserID: "owner",
		}
		ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("x-share-token", "share"))
		_, err := authenticationUnaryInterceptor(&testAuthenticator{session: session}, store, policies, nil)(
			ctx, nil, &grpc.UnaryServerInfo{FullMethod: readMethod},
			func(ctx context.Context, _ any) (any, error) {
				share, ok := pkgauth.ShareContextFrom(ctx)
				if !ok {
					t.Fatal("no share context")
				}
				if !share.IsForSession(testSessionID.String()) {
					t.Errorf("share is not for session-1: %#v", share)
				}
				// The owner, not the visitor: the session read runs as the owner or
				// it finds nothing, because a session is scoped to its creator.
				if share.UserID != "owner" {
					t.Errorf("UserID = %q, want the owner", share.UserID)
				}
				return nil, nil
			},
		)
		if err != nil {
			t.Fatalf("interceptor error = %v", err)
		}
	})

	t.Run("a read-only Session share cannot create a catalog resource", func(t *testing.T) {
		store := &testShareStore{
			sessionShare: &apiv1.SessionShare{SessionId: testSessionID.String()}, ownerUserID: "owner",
		}
		ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("x-share-token", "share"))
		_, err := authenticationUnaryInterceptor(&testAuthenticator{session: session}, store, policies, nil)(
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

	t.Run("invalid share token is denied", func(t *testing.T) {
		store := &testShareStore{sessionShareErr: database.ErrNotFound}
		ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("x-share-token", "missing"))
		_, err := authenticationUnaryInterceptor(&testAuthenticator{session: session}, store, policies, nil)(
			ctx, nil, &grpc.UnaryServerInfo{FullMethod: readMethod},
			func(context.Context, any) (any, error) { return nil, nil },
		)
		if got := status.Code(err); got != codes.PermissionDenied {
			t.Fatalf("code = %v, want PermissionDenied", got)
		}
	})
}

func TestA2AShareAuthorizationIsDelegatedToGateway(t *testing.T) {
	session := &testSession{principal: pkgauth.Principal{ID: "visitor"}}
	store := &testShareStore{
		sessionShare: &apiv1.SessionShare{
			SessionId: testSessionID.String(),
		},
		ownerUserID: "owner",
	}
	for _, method := range []string{
		a2apb.A2AService_SendMessage_FullMethodName,
		a2apb.A2AService_SendStreamingMessage_FullMethodName,
		a2apb.A2AService_CancelTask_FullMethodName,
	} {
		t.Run(method, func(t *testing.T) {
			ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("x-share-token", "share"))
			ctx, err := authenticate(ctx, method, &testAuthenticator{session: session}, store, DefaultMethodPolicies(), nil)
			if err != nil {
				t.Fatalf("A2A authorization must reach the gateway: %v", err)
			}
			share, ok := pkgauth.ShareContextFrom(ctx)
			if !ok || !share.IsForSession(testSessionID.String()) || share.UserID != "owner" {
				t.Fatalf("validated share = %#v, want read-only authority for its owner and session", share)
			}
			gotSession, ok := pkgauth.AuthSessionFrom(ctx)
			if !ok || gotSession.Principal().ID != "visitor" {
				t.Fatalf("authenticated session = %#v, want the visitor's identity", gotSession)
			}
		})
	}
}

func TestCallerWithoutRuntimeBindingCannotAccessRuntimeStorage(t *testing.T) {
	provider := &testAuthenticator{session: &testSession{principal: pkgauth.Principal{ID: "user"}}}
	_, err := authenticate(t.Context(), runtimev1.TaskStoreService_GetTask_FullMethodName, provider, nil, DefaultMethodPolicies(), nil)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("caller without runtime binding authorized runtime storage: %v", err)
	}
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
		{"service wraps backend status", serviceerrors.NewUnavailable("unavailable", status.Error(codes.Unknown, "backend detail")), codes.Unavailable},
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
			if test.name == "service wraps backend status" && status.Convert(mapped).Message() != "unavailable" {
				t.Fatalf("backend cause escaped service boundary: %q", status.Convert(mapped).Message())
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
