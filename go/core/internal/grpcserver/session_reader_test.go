package grpcserver

import (
	"context"
	"encoding/base64"
	"testing"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const sessionReaderUser = "system:serviceaccount:agent-platform:klaus-gateway"

// sessionReaderTokenReviewer authenticates every token as the session reader's
// ServiceAccount for the kagent audience.
type sessionReaderTokenReviewer struct{}

func (sessionReaderTokenReviewer) Create(_ context.Context, review *authenticationv1.TokenReview, _ metav1.CreateOptions) (*authenticationv1.TokenReview, error) {
	result := review.DeepCopy()
	result.Status = authenticationv1.TokenReviewStatus{Authenticated: true, Audiences: []string{"kagent"}, User: authenticationv1.UserInfo{Username: sessionReaderUser}}
	return result, nil
}

// The session reader's ServiceAccount reaches SessionService/GetSession as the
// session reader and is refused on every other method, read or write.
func TestSessionReaderIsAdmittedToGetSessionOnly(t *testing.T) {
	authenticator := authimpl.NewProxyAuthenticator("email", authimpl.WithUserIDClaimRequired(),
		authimpl.WithServiceAccountTokenReview(sessionReaderTokenReviewer{}, "kagent"), authimpl.WithSessionReader(sessionReaderUser))
	token := "e30." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"`+sessionReaderUser+`"}`)) + ".signature"
	policies := DefaultMethodPolicies()

	for _, tt := range []struct {
		method string
		want   codes.Code
	}{
		{method: apiv1alpha1.SessionService_GetSession_FullMethodName, want: codes.OK},
		{method: apiv1alpha1.SessionService_ListSessions_FullMethodName, want: codes.PermissionDenied},
		{method: apiv1alpha1.SessionService_CreateSession_FullMethodName, want: codes.PermissionDenied},
		{method: apiv1alpha1.SessionService_DeleteSession_FullMethodName, want: codes.PermissionDenied},
		{method: apiv1alpha1.AgentService_ListAgents_FullMethodName, want: codes.PermissionDenied},
		{method: apiv1alpha1.SystemService_GetCurrentUser_FullMethodName, want: codes.PermissionDenied},
	} {
		t.Run(tt.method, func(t *testing.T) {
			if _, ok := policies[tt.method]; !ok {
				t.Fatalf("no policy for %s", tt.method)
			}
			ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("authorization", "Bearer "+token))
			ran := false
			_, err := authenticationUnaryInterceptor(authenticator, nil, nil, policies)(
				ctx, nil, &grpc.UnaryServerInfo{FullMethod: tt.method},
				func(ctx context.Context, _ any) (any, error) {
					ran = true
					session, _ := pkgauth.AuthSessionFrom(ctx)
					if _, ok := session.(pkgauth.SessionReaderSession); !ok || session.Principal().User.ID != sessionReaderUser {
						t.Fatalf("session = %#v, want the session reader", session)
					}
					return nil, nil
				},
			)
			if got := status.Code(err); got != tt.want {
				t.Fatalf("code = %v, want %v", got, tt.want)
			}
			if ran != (tt.want == codes.OK) {
				t.Fatalf("handler ran = %v", ran)
			}
		})
	}
}
