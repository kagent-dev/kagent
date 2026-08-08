package e2e_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// makeTestJWT builds a minimal unsigned JWT (alg:none) with the given claims.
// This is sufficient for trusted-proxy mode testing where the oauth2-proxy has already
// validated the token and the backend only parses claims without verification.
func makeTestJWT(claims map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, _ := json.Marshal(claims)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	return header + "." + payloadB64 + "."
}

// getCurrentUser calls SystemService.GetCurrentUser (the identity surface
// moved from GET /api/me to gRPC) with the given metadata pairs and returns
// the raw claims map. The server forwards the authorization / x-user-id /
// x-agent-name metadata keys to the same authenticators the HTTP endpoints
// used.
func getCurrentUser(t *testing.T, metadataPairs ...string) (map[string]any, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if len(metadataPairs) > 0 {
		ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(metadataPairs...))
	}

	client := apiv1alpha1.NewSystemServiceClient(newE2EGRPCConn(t))
	response, err := client.GetCurrentUser(ctx, &apiv1alpha1.GetCurrentUserRequest{})
	if err != nil {
		return nil, err
	}
	return response.GetClaims().AsMap(), nil
}

// detectAuthMode probes GetCurrentUser to determine if the deployment is in trusted-proxy or unsecure mode.
// Sends a JWT Bearer token; in trusted-proxy mode the backend parses the JWT and returns the sub claim.
// In unsecure mode the backend ignores the Bearer token and returns the default user.
// Returns "trusted-proxy" if trusted-proxy mode, "unsecure" otherwise.
func detectAuthMode(t *testing.T) string {
	t.Helper()

	token := makeTestJWT(map[string]any{"sub": "probe-user"})
	claims, err := getCurrentUser(t, "authorization", "Bearer "+token)
	if err == nil {
		if sub, _ := claims["sub"].(string); sub == "probe-user" {
			return "trusted-proxy"
		}
	}
	return "unsecure"
}

func TestE2EAuthUnsecureMode(t *testing.T) {
	// Skip if deployment is in proxy mode
	if detectAuthMode(t) == "trusted-proxy" {
		t.Skip("Skipping unsecure mode tests - deployment is in trusted-proxy mode")
	}

	t.Run("default_user", func(t *testing.T) {
		// GetCurrentUser with no auth metadata should return the default user
		claims, err := getCurrentUser(t)
		require.NoError(t, err)
		require.Equal(t, "admin@kagent.dev", claims["sub"])
	})

	t.Run("x_user_id_metadata", func(t *testing.T) {
		// GetCurrentUser with x-user-id metadata should return that user
		claims, err := getCurrentUser(t, "x-user-id", "alice@example.com")
		require.NoError(t, err)
		require.Equal(t, "alice@example.com", claims["sub"])
	})
}

func TestE2EAuthProxyMode(t *testing.T) {
	// Skip if deployment is not in trusted-proxy mode
	if detectAuthMode(t) != "trusted-proxy" {
		t.Skip("Skipping trusted-proxy mode tests - deployment is in unsecure mode")
	}

	t.Run("full_claims", func(t *testing.T) {
		// JWT with all standard claims
		token := makeTestJWT(map[string]any{
			"sub":    "john",
			"email":  "john@example.com",
			"name":   "John Doe",
			"groups": []string{"admin", "developers"},
		})
		claims, err := getCurrentUser(t, "authorization", "Bearer "+token)
		require.NoError(t, err)
		require.Equal(t, "john", claims["sub"])
		require.Equal(t, "john@example.com", claims["email"])
		require.Equal(t, "John Doe", claims["name"])
		// Groups come through as raw claim
		groups, ok := claims["groups"].([]any)
		require.True(t, ok, "groups should be an array")
		require.Len(t, groups, 2)
	})

	t.Run("minimal_claims", func(t *testing.T) {
		// JWT with only sub claim
		token := makeTestJWT(map[string]any{
			"sub": "jane",
		})
		claims, err := getCurrentUser(t, "authorization", "Bearer "+token)
		require.NoError(t, err)
		require.Equal(t, "jane", claims["sub"])
		require.Nil(t, claims["email"])
		require.Nil(t, claims["name"])
		require.Nil(t, claims["groups"])
	})

	t.Run("missing_sub_claim_unauthenticated", func(t *testing.T) {
		// JWT without sub claim should be rejected
		token := makeTestJWT(map[string]any{
			"email": "test@example.com",
		})
		_, err := getCurrentUser(t, "authorization", "Bearer "+token)
		require.Equal(t, codes.Unauthenticated, status.Code(err))
	})

	t.Run("no_bearer_token_unauthenticated", func(t *testing.T) {
		// No authorization metadata and no agent identity should be rejected
		_, err := getCurrentUser(t)
		require.Equal(t, codes.Unauthenticated, status.Code(err))
	})

	t.Run("agent_fallback_with_user_id", func(t *testing.T) {
		// Agent callback: SA Bearer token + x-agent-name authenticate the pod;
		// the caller identity is supplied via x-user-id
		token := makeTestJWT(map[string]any{"sub": "system:serviceaccount:kagent:test-agent"})
		claims, err := getCurrentUser(t,
			"authorization", "Bearer "+token,
			"x-agent-name", "kagent/test-agent",
			"x-user-id", "owner@example.com",
		)
		require.NoError(t, err)
		require.Equal(t, "owner@example.com", claims["sub"])
	})

	t.Run("fallback_without_bearer_unauthenticated", func(t *testing.T) {
		// x-user-id without a Bearer token should be rejected
		_, err := getCurrentUser(t, "x-user-id", "owner@example.com")
		require.Equal(t, codes.Unauthenticated, status.Code(err))
	})
}
