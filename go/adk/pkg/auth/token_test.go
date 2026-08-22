package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithUserIDAndUserIDFromContext(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, "", UserIDFromContext(ctx))

	ctx = WithUserID(ctx, "user-123")
	assert.Equal(t, "user-123", UserIDFromContext(ctx))
}

func TestKAgentTokenService_GetToken_InitiallyEmpty(t *testing.T) {
	svc := NewKAgentTokenService("my-agent")
	assert.Equal(t, "", svc.GetToken())
}

func TestKAgentTokenService_AddHeaders(t *testing.T) {
	svc := NewKAgentTokenService("my-agent")
	svc.mu.Lock()
	svc.token = "abc123"
	svc.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	req = req.WithContext(WithUserID(req.Context(), "user-42"))

	svc.AddHeaders(req)

	assert.Equal(t, "my-agent", req.Header.Get("X-Agent-Name"))
	assert.Equal(t, "Bearer abc123", req.Header.Get("Authorization"))
	assert.Equal(t, "user-42", req.Header.Get("X-User-Id"))
}

func TestKAgentTokenService_AddHeaders_NoTokenNoUser(t *testing.T) {
	svc := NewKAgentTokenService("my-agent")
	req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)

	svc.AddHeaders(req)

	assert.Equal(t, "my-agent", req.Header.Get("X-Agent-Name"))
	assert.Equal(t, "", req.Header.Get("Authorization"))
	assert.Equal(t, "", req.Header.Get("X-User-Id"))
}

func TestKAgentTokenService_Stop_SafeMultipleCalls(t *testing.T) {
	svc := NewKAgentTokenService("my-agent")
	assert.NotPanics(t, func() {
		svc.Stop()
		svc.Stop()
		svc.Stop()
	})
}

func TestKAgentTokenService_Start_MissingTokenFile(t *testing.T) {
	svc := NewKAgentTokenService("my-agent")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	err := svc.Start(ctx)
	require.NoError(t, err)
	assert.Equal(t, "", svc.GetToken())

	svc.Stop()
}

func TestKAgentTokenService_ReadToken_MissingFile(t *testing.T) {
	svc := NewKAgentTokenService("my-agent")
	token, err := svc.readToken()
	assert.Error(t, err)
	assert.Empty(t, token)
}

type stubRoundTripper struct {
	req  *http.Request
	resp *http.Response
	err  error
}

func (s *stubRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	s.req = req
	if s.resp != nil {
		return s.resp, s.err
	}
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, s.err
}

func TestTokenRoundTripper_InjectsHeaders(t *testing.T) {
	svc := NewKAgentTokenService("my-agent")
	svc.mu.Lock()
	svc.token = "xyz"
	svc.mu.Unlock()

	base := &stubRoundTripper{}
	rt := &TokenRoundTripper{base: base, tokenService: svc}

	req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	resp, err := rt.RoundTrip(req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "Bearer xyz", base.req.Header.Get("Authorization"))
	assert.Equal(t, "my-agent", base.req.Header.Get("X-Agent-Name"))
}

func TestTokenRoundTripper_NilTokenService(t *testing.T) {
	base := &stubRoundTripper{}
	rt := &TokenRoundTripper{base: base, tokenService: nil}

	req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	_, err := rt.RoundTrip(req)

	require.NoError(t, err)
	assert.Equal(t, "", base.req.Header.Get("Authorization"))
}

func TestNewHTTPClientWithToken(t *testing.T) {
	svc := NewKAgentTokenService("my-agent")
	client := NewHTTPClientWithToken(svc)

	require.NotNil(t, client)
	rt, ok := client.Transport.(*TokenRoundTripper)
	require.True(t, ok)
	assert.Equal(t, svc, rt.tokenService)
	assert.Equal(t, 30*time.Second, client.Timeout)
}
