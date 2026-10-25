package invocationauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/require"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

func invocationContext(t *testing.T, token, controlPlaneURL string) context.Context {
	t.Helper()
	params := map[string][]string{}
	if token != "" {
		params["Authorization"] = []string{"Bearer " + token}
	}
	ctx, call := a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(params))
	ctx, _, err := PolicyCallInterceptor(controlPlaneURL).Before(ctx, call, nil)
	require.NoError(t, err)
	return ctx
}

func TestSourceKeepsTheOpeningCaller(t *testing.T) {
	bindings := []*configv1.InvocationCredentialHeader{{Name: "Authorization", Prefix: "Bearer "}, {Name: "X-Caller"}}
	source, err := FromContext(invocationContext(t, "machine", ""))
	require.NoError(t, err)
	other := invocationContext(t, "later-human", "")
	request := httptest.NewRequest(http.MethodPost, "https://tools.example/mcp", nil).WithContext(other)
	require.NoError(t, source.ApplyRequest(other, request, bindings))
	require.Equal(t, "Bearer machine", request.Header.Get("Authorization"))
	require.Equal(t, "machine", request.Header.Get("X-Caller"))
	require.NoError(t, ApplyRequest(other, request, bindings))
	require.Equal(t, "Bearer later-human", request.Header.Get("Authorization"))
}

func TestLivePolicyUsesOnlyLocalCallerToken(t *testing.T) {
	bindings := []*configv1.InvocationCredentialHeader{{Name: "Authorization", Prefix: "Bearer "}}
	targets := []Target{{Origin: "https://tools.example:443", Passthrough: bindings}}
	calls := 0
	policy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, EgressPolicyPath, r.URL.Path)
		require.Equal(t, "Bearer kagent-credential-injected", r.Header.Get("Authorization"))
		require.NoError(t, json.NewEncoder(w).Encode(targets))
	}))
	defer policy.Close()
	ctx := invocationContext(t, "machine", policy.URL)
	source, err := FromContext(ctx)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "https://tools.example/mcp", nil)
	require.NoError(t, source.ApplyRequest(ctx, request, bindings))
	require.Equal(t, "Bearer machine", request.Header.Get("Authorization"))
	targets = nil
	request = httptest.NewRequest(http.MethodPost, "https://tools.example/mcp", nil)
	require.ErrorContains(t, source.ApplyRequest(ctx, request, bindings), "binding unavailable")
	require.Empty(t, request.Header.Get("Authorization"))
	require.Equal(t, 2, calls, "there is one policy call per send and no credential retrieval")
}

func TestPolicyFailureNeverFallsBackToPinnedBindings(t *testing.T) {
	for _, code := range []int{http.StatusForbidden, http.StatusServiceUnavailable, http.StatusTemporaryRedirect} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			policy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "https://outside.example")
				w.WriteHeader(code)
			}))
			defer policy.Close()
			ctx := invocationContext(t, "caller", policy.URL)
			request := httptest.NewRequest(http.MethodPost, "https://tools.example/mcp", nil)
			err := ApplyRequest(ctx, request, []*configv1.InvocationCredentialHeader{{Name: "Authorization", Prefix: "Bearer "}})
			require.Error(t, err)
			require.Empty(t, request.Header.Get("Authorization"))
		})
	}
}

func TestMissingAndMalformedInvocationCredentials(t *testing.T) {
	for _, values := range [][]string{nil, {"Bearer "}, {"Bearer one", "Bearer two"}, {"Basic secret"}, {"Bearer kagent-credential-injected"}} {
		ctx, _ := a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(map[string][]string{"Authorization": values}))
		_, err := FromContext(ctx)
		require.Error(t, err)
	}
	ctx := invocationContext(t, "", "")
	request := httptest.NewRequest(http.MethodPost, "https://tools.example/mcp", nil)
	require.ErrorContains(t, ApplyRequest(ctx, request, []*configv1.InvocationCredentialHeader{{Name: "Authorization"}}), "credential unavailable")
	require.NoError(t, ApplyRequest(ctx, request, nil), "Secret and Actor token requests need no caller")
}

func TestLivePolicyCanonicalOrigins(t *testing.T) {
	bindings := []*configv1.InvocationCredentialHeader{{Name: "Authorization", Prefix: "Bearer "}}
	policy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewEncoder(w).Encode([]Target{{Origin: "https://tools.example:443", Passthrough: bindings}}))
	}))
	defer policy.Close()
	ctx := invocationContext(t, "caller", policy.URL)
	for _, endpoint := range []string{"https://TOOLS.EXAMPLE/mcp", "https://tools.example./mcp", "https://tools.example:0443/mcp", "https://tools.example.:443/mcp"} {
		t.Run(endpoint, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, endpoint, nil)
			require.NoError(t, ApplyRequest(ctx, request, bindings))
			require.Equal(t, "Bearer caller", request.Header.Get("Authorization"))
		})
	}
	for _, endpoint := range []string{"http://tools.example:443/mcp", "https://tools.example:8443/mcp", "https://other.example/mcp"} {
		request := httptest.NewRequest(http.MethodPost, endpoint, nil)
		require.Error(t, ApplyRequest(ctx, request, bindings))
		require.Empty(t, request.Header.Get("Authorization"))
	}
}

func TestLivePolicyAcceptsLargeAdmittedPolicies(t *testing.T) {
	var targets []Target
	for destination := range 16 {
		target := Target{Origin: fmt.Sprintf("https://tools-%d.example:443", destination)}
		for header := range 16 {
			target.Passthrough = append(target.Passthrough, &configv1.InvocationCredentialHeader{Name: fmt.Sprintf("X-Caller-%d", header), Prefix: strings.Repeat("a", 256)})
		}
		targets = append(targets, target)
	}
	data, err := json.Marshal(targets)
	require.NoError(t, err)
	require.Greater(t, len(data), 65536)
	policy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(data) }))
	defer policy.Close()
	ctx := invocationContext(t, "caller", policy.URL)
	request := httptest.NewRequest(http.MethodPost, "https://tools-15.example/mcp", nil)
	require.NoError(t, ApplyRequest(ctx, request, targets[15].Passthrough))
	require.Equal(t, strings.Repeat("a", 256)+"caller", request.Header.Get("X-Caller-15"))
}
