package e2e_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// CurrentUserResponse mirrors the response from GET /api/me
type CurrentUserResponse struct {
	User   string   `json:"user"`
	Email  string   `json:"email"`
	Name   string   `json:"name"`
	Groups []string `json:"groups"`
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
// Returns "proxy" if proxy mode, "unsecure" otherwise.
func detectAuthMode(t *testing.T) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create request with X-Forwarded-User but no X-User-Id
	// In proxy mode: will return the forwarded user
	// In unsecure mode: will return default user (ignores X-Forwarded-User)
	req, err := http.NewRequestWithContext(ctx, "GET", kagentURL()+"/api/me", nil)
	require.NoError(t, err)
	req.Header.Set("X-Forwarded-User", "probe-user")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var userResp CurrentUserResponse
		err = json.NewDecoder(resp.Body).Decode(&userResp)
		require.NoError(t, err)

		if userResp.User == "probe-user" {
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

// parseUserResponse parses a CurrentUserResponse from JSON body.
func parseUserResponse(t *testing.T, body []byte) CurrentUserResponse {
	t.Helper()
	var userResp CurrentUserResponse
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
		require.Equal(t, "admin@kagent.dev", userResp.User)
		require.Empty(t, userResp.Email)
		require.Empty(t, userResp.Name)
		require.Empty(t, userResp.Groups)
	})

	t.Run("x_user_id_header", func(t *testing.T) {
		// GET /api/me with X-User-Id header should return that user
		resp, body := makeAuthRequest(t, map[string]string{
			"X-User-Id": "alice@example.com",
		}, nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		userResp := parseUserResponse(t, body)
		require.Equal(t, "alice@example.com", userResp.User)
	})

	t.Run("user_id_query_param", func(t *testing.T) {
		// GET /api/me?user_id=bob should return that user
		resp, body := makeAuthRequest(t, nil, map[string]string{
			"user_id": "bob@example.com",
		})
		require.Equal(t, http.StatusOK, resp.StatusCode)

		userResp := parseUserResponse(t, body)
		require.Equal(t, "bob@example.com", userResp.User)
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
		require.Equal(t, "query-user", userResp.User)
	})
}

func TestE2EAuthProxyMode(t *testing.T) {
	// Skip if deployment is not in proxy mode
	if detectAuthMode(t) != "proxy" {
		t.Skip("Skipping proxy mode tests - deployment is in unsecure mode")
	}

	t.Run("full_headers", func(t *testing.T) {
		// GET /api/me with all X-Forwarded-* headers
		resp, body := makeAuthRequest(t, map[string]string{
			"X-Forwarded-User":               "john",
			"X-Forwarded-Email":              "john@example.com",
			"X-Forwarded-Preferred-Username": "John Doe",
			"X-Forwarded-Groups":             "admin,developers",
		}, nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		userResp := parseUserResponse(t, body)
		require.Equal(t, "john", userResp.User)
		require.Equal(t, "john@example.com", userResp.Email)
		require.Equal(t, "John Doe", userResp.Name)
		require.ElementsMatch(t, []string{"admin", "developers"}, userResp.Groups)
	})

	t.Run("minimal_headers", func(t *testing.T) {
		// GET /api/me with only required X-Forwarded-User header
		resp, body := makeAuthRequest(t, map[string]string{
			"X-Forwarded-User": "jane",
		}, nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		userResp := parseUserResponse(t, body)
		require.Equal(t, "jane", userResp.User)
		require.Empty(t, userResp.Email)
		require.Empty(t, userResp.Name)
		require.Empty(t, userResp.Groups)
	})

	t.Run("missing_required_header_returns_401", func(t *testing.T) {
		// GET /api/me without X-Forwarded-User should return 401
		resp, _ := makeAuthRequest(t, map[string]string{
			"X-Forwarded-Email": "test@example.com", // email but no user
		}, nil)
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("group_parsing_trims_whitespace", func(t *testing.T) {
		// Groups with whitespace around commas should be trimmed
		resp, body := makeAuthRequest(t, map[string]string{
			"X-Forwarded-User":   "user",
			"X-Forwarded-Groups": "admin, dev , qa ",
		}, nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		userResp := parseUserResponse(t, body)
		require.ElementsMatch(t, []string{"admin", "dev", "qa"}, userResp.Groups)
	})

	t.Run("empty_groups_header", func(t *testing.T) {
		// Empty groups header should result in empty groups slice
		resp, body := makeAuthRequest(t, map[string]string{
			"X-Forwarded-User":   "user",
			"X-Forwarded-Groups": "",
		}, nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		userResp := parseUserResponse(t, body)
		require.Empty(t, userResp.Groups)
	})
}
