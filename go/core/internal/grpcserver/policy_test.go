package grpcserver

import (
	"testing"
	"time"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/jwttest"
	"github.com/kagent-dev/kagent/go/core/internal/runtimeauth"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// TestSessionServicePoliciesMatchTheirEffect pins the access mode of every
// method on the service. A wrong mode here fails silently in one of two ways: a
// write classified as a read is reachable through a read-only share link, and a
// read classified as a write is refused for a legitimate caller. Neither shows
// up as an error anywhere near the policy table.
func TestSessionServicePoliciesMatchTheirEffect(t *testing.T) {
	policies := DefaultMethodPolicies()
	for _, test := range []struct {
		name   string
		method string
		want   pkgauth.AccessMode
	}{
		{name: "create", method: apiv1.SessionService_CreateSession_FullMethodName, want: pkgauth.AccessCreate},
		{name: "get", method: apiv1.SessionService_GetSession_FullMethodName, want: pkgauth.AccessRead},
		{name: "list", method: apiv1.SessionService_ListSessions_FullMethodName, want: pkgauth.AccessRead},
		{name: "rename is a write", method: apiv1.SessionService_UpdateSessionName_FullMethodName, want: pkgauth.AccessUpdate},
		{name: "suspend", method: apiv1.SessionService_SuspendSession_FullMethodName, want: pkgauth.AccessUpdate},
		{name: "resume", method: apiv1.SessionService_ResumeSession_FullMethodName, want: pkgauth.AccessUpdate},
		{name: "delete", method: apiv1.SessionService_DeleteSession_FullMethodName, want: pkgauth.AccessDelete},
		{name: "create share", method: apiv1.SessionService_CreateSessionShare_FullMethodName, want: pkgauth.AccessCreate},
		{name: "list shares", method: apiv1.SessionService_ListSessionShares_FullMethodName, want: pkgauth.AccessRead},
		{name: "revoke share", method: apiv1.SessionService_RevokeSessionShare_FullMethodName, want: pkgauth.AccessDelete},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := policies[test.method]
			if !ok {
				t.Fatalf("%s has no policy; an unconfigured method is denied outright", test.method)
			}
			if got != test.want {
				t.Fatalf("%s policy = %q, want %q", test.method, got, test.want)
			}
		})
	}
}

// TestReadOnlyShareCannotRenameAConversation is the property the policy entry
// exists for, measured through the interceptor rather than read off the table: a
// read-only share link may open a conversation and must not be able to retitle
// it for everyone who holds the link.
func TestReadOnlyShareCannotRenameAConversation(t *testing.T) {
	session := &testSession{principal: pkgauth.Principal{ID: "visitor"}}
	for _, test := range []struct {
		name     string
		method   string
		wantCode codes.Code
	}{
		{
			name:   "read-only share may read",
			method: apiv1.SessionService_GetSession_FullMethodName, wantCode: codes.OK,
		},
		{
			name:   "read-only share may not rename",
			method: apiv1.SessionService_UpdateSessionName_FullMethodName, wantCode: codes.PermissionDenied,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			shareStore := &testShareStore{
				sessionShare: &apiv1.SessionShare{SessionId: testSessionID.String()}, ownerUserID: "owner",
			}
			ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("x-share-token", "token"))
			_, err := authenticate(ctx, test.method, &testAuthenticator{session: session}, shareStore, DefaultMethodPolicies())
			if got := status.Code(err); got != test.wantCode {
				t.Fatalf("authenticate(%s) code = %v (%v), want %v", test.method, got, err, test.wantCode)
			}
		})
	}
}

func TestSandboxGuestPoliciesMatchTheirEffect(t *testing.T) {
	policies := DefaultMethodPolicies()
	for method, expected := range map[string]pkgauth.AccessMode{
		guestpb.ProcessService_StartProcess_FullMethodName:        pkgauth.AccessCreate,
		guestpb.ProcessService_GetProcess_FullMethodName:          pkgauth.AccessRead,
		guestpb.ProcessService_SignalProcess_FullMethodName:       pkgauth.AccessUpdate,
		guestpb.ProcessService_StreamProcessOutput_FullMethodName: pkgauth.AccessRead,
		guestpb.FileSystemService_ReadFile_FullMethodName:         pkgauth.AccessRead,
		guestpb.FileSystemService_WriteFile_FullMethodName:        pkgauth.AccessUpdate,
	} {
		t.Run(method, func(t *testing.T) {
			if actual, ok := policies[method]; !ok || actual != expected {
				t.Fatalf("policy = %v (configured: %v), want %v", actual, ok, expected)
			}
		})
	}
}

func TestActorAndUserPoliciesOnSharedEndpoint(t *testing.T) {
	issuer := jwttest.New(t)
	verifier, err := substrate.NewActorJWTVerifier(t.Context(), issuer.URL, issuer.CAFile)
	require.NoError(t, err)
	sessionID := uuid.NewString()
	store := &memoryRPCStore{session: &apiv1.Session{Id: sessionID, Creator: "owner", Agent: &apiv1.ResourceReference{Namespace: "team-a", Name: "agent"}, A2AAuthority: substrate.ActorHost("team-a", substrate.ActorName(sessionID), "")}}
	claims := issuer.Claims("team-a", "session-"+sessionID, "actor-uid")
	actor := "Bearer " + issuer.Token(t, claims)
	claims["exp"] = time.Now().Add(-time.Minute).Unix()
	expired := "Bearer " + issuer.Token(t, claims)
	forged := "Bearer " + issuer.Token(t, jwt.MapClaims{"iss": issuer.URL, "sub": "alice"})
	user := "Bearer " + issuer.Token(t, jwt.MapClaims{"iss": "https://users.example", "sub": "alice"})
	policies := DefaultMethodPolicies()
	policies["/extension.Service/Call"] = pkgauth.AccessRead
	for _, mode := range []struct {
		name        string
		provider    pkgauth.AuthProvider
		anonymous   codes.Code
		runtimeCode codes.Code
	}{
		{"insecure", &authimpl.InsecureAuthenticator{}, codes.OK, codes.PermissionDenied},
		{"trusted proxy", authimpl.NewProxyAuthenticator("sub"), codes.Unauthenticated, codes.Unauthenticated},
	} {
		t.Run(mode.name, func(t *testing.T) {
			users := runtimeauth.NewProvider(mode.provider, verifier, store, issuer.URL)
			for _, test := range []struct {
				name, token, method string
				want                codes.Code
			}{
				{"actor task", actor, runtimev1.TaskStoreService_GetTask_FullMethodName, codes.OK},
				{"actor memory list", actor, runtimev1.MemoryService_List_FullMethodName, codes.OK},
				{"actor memory delete", actor, runtimev1.MemoryService_Delete_FullMethodName, codes.OK},
				{"user memory list", user, runtimev1.MemoryService_List_FullMethodName, codes.OK},
				{"user memory delete", user, runtimev1.MemoryService_Delete_FullMethodName, codes.OK},
				{"user task denied", user, runtimev1.TaskStoreService_GetTask_FullMethodName, codes.PermissionDenied},
				{"anonymous task denied", "", runtimev1.TaskStoreService_GetTask_FullMethodName, mode.runtimeCode},
				{"anonymous memory uses configured auth", "", runtimev1.MemoryService_List_FullMethodName, mode.anonymous},
				{"anonymous user API uses configured auth", "", apiv1.SessionService_ListSessions_FullMethodName, mode.anonymous},
				{"anonymous public API", "", apiv1.SystemService_GetVersion_FullMethodName, codes.OK},
				{"actor user API delegates authorization", actor, apiv1.SessionService_CreateSession_FullMethodName, codes.OK},
				{"actor public API delegates authorization", actor, apiv1.SystemService_GetVersion_FullMethodName, codes.OK},
				{"actor A2A delegates authorization", actor, a2apb.A2AService_SendMessage_FullMethodName, codes.OK},
				{"actor streaming A2A delegates authorization", actor, a2apb.A2AService_SendStreamingMessage_FullMethodName, codes.OK},
				{"actor extension delegates authorization", actor, "/extension.Service/Call", codes.OK},
				{"unknown method denied", actor, "/unconfigured.Service/Call", codes.PermissionDenied},
				{"expired actor has no user fallback", expired, runtimev1.MemoryService_List_FullMethodName, codes.Unauthenticated},
				{"forged actor has no user fallback", forged, runtimev1.MemoryService_Delete_FullMethodName, codes.Unauthenticated},
				{"expired actor cannot use public API", expired, apiv1.SystemService_GetVersion_FullMethodName, codes.Unauthenticated},
			} {
				t.Run(test.name, func(t *testing.T) {
					md := metadata.Pairs("x-user-id", "alice")
					if test.token != "" {
						md.Set("authorization", test.token)
					}
					ctx := metadata.NewIncomingContext(t.Context(), md)
					result, err := authenticate(ctx, test.method, users, nil, policies)
					require.Equal(t, test.want, status.Code(err), "%v", err)
					if test.want == codes.OK && test.token == actor {
						identity, ok := pkgauth.AuthSessionFrom(result)
						require.True(t, ok)
						require.Equal(t, pkgauth.PrincipalID("substrate-actor", "team-a/actor-uid"), identity.Principal().ID)
						require.Equal(t, sessionID, identity.Principal().RuntimeBinding.SessionID)
					}
				})
			}
			shared := metadata.NewIncomingContext(t.Context(), metadata.Pairs("authorization", actor, "x-share-token", "share"))
			_, err := authenticate(shared, runtimev1.MemoryService_List_FullMethodName, users, nil, policies)
			require.Equal(t, codes.Unauthenticated, status.Code(err))
		})
	}
}
