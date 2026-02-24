package e2e_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// makeTestJWT builds a minimal unsigned JWT (alg:none) with the given claims.
// This is sufficient for proxy mode testing where the oauth2-proxy has already
// validated the token and the backend only parses claims without verification.
func makeTestJWT(claims map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, _ := json.Marshal(claims)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	return header + "." + payloadB64 + "."
}

// kagentURL returns the base URL for kagent API.
// Configurable via KAGENT_URL env var.
func kagentURL() string {
	if url := os.Getenv("KAGENT_URL"); url != "" {
		return url
	}
	return "http://localhost:8083"
}

// detectAuthMode probes /api/me to determine if the deployment is in proxy or unsecure mode.
// Sends a JWT Bearer token; in proxy mode the backend parses the JWT and returns the sub claim.
// In unsecure mode the backend ignores the Bearer token and returns the default user.
// Returns "proxy" if proxy mode, "unsecure" otherwise.
func detectAuthMode(t *testing.T) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	token := makeTestJWT(map[string]any{"sub": "probe-user"})
	req, err := http.NewRequestWithContext(ctx, "GET", kagentURL()+"/api/me", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var userResp map[string]any
		err = json.NewDecoder(resp.Body).Decode(&userResp)
		require.NoError(t, err)

		if sub, _ := userResp["sub"].(string); sub == "probe-user" {
			return "proxy"
		}
	}
	return "unsecure"
}

// makeAuthRequest makes a GET request to /api/me with optional headers and query params.
func makeAuthRequest(t *testing.T, headers map[string]string, queryParams map[string]string) (*http.Response, []byte) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reqURL := kagentURL() + "/api/me"
	if len(queryParams) > 0 {
		var sb strings.Builder
		sb.WriteString(reqURL)
		sb.WriteString("?")
		first := true
		for k, v := range queryParams {
			if !first {
				sb.WriteString("&")
			}
			sb.WriteString(k)
			sb.WriteString("=")
			sb.WriteString(v)
			first = false
		}
		reqURL = sb.String()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	require.NoError(t, err)

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	resp.Body.Close()

	return resp, body
}

// parseUserResponse parses a raw claims map from JSON body.
func parseUserResponse(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var userResp map[string]any
	err := json.Unmarshal(body, &userResp)
	require.NoError(t, err)
	return userResp
}

func TestE2EAuthUnsecureMode(t *testing.T) {
	// Skip if deployment is in proxy mode
	if detectAuthMode(t) == "proxy" {
		t.Skip("Skipping unsecure mode tests - deployment is in proxy mode")
	}

	t.Run("default_user", func(t *testing.T) {
		// GET /api/me with no auth headers should return default user
		resp, body := makeAuthRequest(t, nil, nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		userResp := parseUserResponse(t, body)
		require.Equal(t, "admin@kagent.dev", userResp["sub"])
	})

	t.Run("x_user_id_header", func(t *testing.T) {
		// GET /api/me with X-User-Id header should return that user
		resp, body := makeAuthRequest(t, map[string]string{
			"X-User-Id": "alice@example.com",
		}, nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		userResp := parseUserResponse(t, body)
		require.Equal(t, "alice@example.com", userResp["sub"])
	})

	t.Run("user_id_query_param", func(t *testing.T) {
		// GET /api/me?user_id=bob should return that user
		resp, body := makeAuthRequest(t, nil, map[string]string{
			"user_id": "bob@example.com",
		})
		require.Equal(t, http.StatusOK, resp.StatusCode)

		userResp := parseUserResponse(t, body)
		require.Equal(t, "bob@example.com", userResp["sub"])
	})

	t.Run("header_takes_precedence_over_query", func(t *testing.T) {
		// When both header and query param are present, query param takes precedence
		// (based on UnsecureAuthenticator implementation which checks query first)
		resp, body := makeAuthRequest(t, map[string]string{
			"X-User-Id": "header-user",
		}, map[string]string{
			"user_id": "query-user",
		})
		require.Equal(t, http.StatusOK, resp.StatusCode)

		userResp := parseUserResponse(t, body)
		require.Equal(t, "query-user", userResp["sub"])
	})
}

func TestE2EAuthProxyMode(t *testing.T) {
	// Skip if deployment is not in proxy mode
	if detectAuthMode(t) != "proxy" {
		t.Skip("Skipping proxy mode tests - deployment is in unsecure mode")
	}

	t.Run("full_claims", func(t *testing.T) {
		// JWT with all standard claims
		token := makeTestJWT(map[string]any{
			"sub":    "john",
			"email":  "john@example.com",
			"name":   "John Doe",
			"groups": []string{"admin", "developers"},
		})
		resp, body := makeAuthRequest(t, map[string]string{
			"Authorization": "Bearer " + token,
		}, nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		userResp := parseUserResponse(t, body)
		require.Equal(t, "john", userResp["sub"])
		require.Equal(t, "john@example.com", userResp["email"])
		require.Equal(t, "John Doe", userResp["name"])
		// Groups come through as raw claim
		groups, ok := userResp["groups"].([]any)
		require.True(t, ok, "groups should be an array")
		require.Len(t, groups, 2)
	})

	t.Run("minimal_claims", func(t *testing.T) {
		// JWT with only sub claim
		token := makeTestJWT(map[string]any{
			"sub": "jane",
		})
		resp, body := makeAuthRequest(t, map[string]string{
			"Authorization": "Bearer " + token,
		}, nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		userResp := parseUserResponse(t, body)
		require.Equal(t, "jane", userResp["sub"])
		require.Nil(t, userResp["email"])
		require.Nil(t, userResp["name"])
		require.Nil(t, userResp["groups"])
	})

	t.Run("missing_sub_claim_returns_401", func(t *testing.T) {
		// JWT without sub claim should return 401
		token := makeTestJWT(map[string]any{
			"email": "test@example.com",
		})
		resp, _ := makeAuthRequest(t, map[string]string{
			"Authorization": "Bearer " + token,
		}, nil)
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("no_bearer_token_returns_401", func(t *testing.T) {
		// No Authorization header and no agent identity should return 401
		resp, _ := makeAuthRequest(t, nil, nil)
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("agent_fallback_with_user_id", func(t *testing.T) {
		// Agent callback: X-Agent-Name + user_id query param (no Bearer token)
		resp, body := makeAuthRequest(t, map[string]string{
			"X-Agent-Name": "kagent/test-agent",
		}, map[string]string{
			"user_id": "owner@example.com",
		})
		require.Equal(t, http.StatusOK, resp.StatusCode)

		userResp := parseUserResponse(t, body)
		require.Equal(t, "owner@example.com", userResp["sub"])
	})

	t.Run("fallback_without_agent_name_returns_401", func(t *testing.T) {
		// user_id query param without X-Agent-Name should return 401
		resp, _ := makeAuthRequest(t, nil, map[string]string{
			"user_id": "owner@example.com",
		})
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})
}
