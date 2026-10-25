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
)

func invocationContext(t *testing.T, token string, targets []Target) context.Context {
	t.Helper()
	params := map[string][]string{}
	if token != "" {
		params["Authorization"] = []string{"Bearer " + token}
	}
	if targets != nil {
		data, err := json.Marshal(targets)
		require.NoError(t, err)
		params[HeadersMetadataKey] = []string{string(data)}
	}
	ctx, _ := a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(params))
	return ctx
}

func TestSourceKeepsTheOpeningCaller(t *testing.T) {
	bindings := []Header{{Name: "Authorization", Prefix: "Bearer "}, {Name: "X-Caller"}}
	source, err := FromContext(invocationContext(t, "machine", []Target{{Origin: "https://tools.example:443", Passthrough: bindings}}))
	require.NoError(t, err)
	other := invocationContext(t, "later-human", []Target{{Origin: "https://tools.example:443", Passthrough: bindings}})
	request := httptest.NewRequest(http.MethodPost, "https://tools.example/mcp", nil).WithContext(other)
	require.NoError(t, source.ApplyRequest(request))
	require.Equal(t, "Bearer machine", request.Header.Get("Authorization"))
	require.Equal(t, "machine", request.Header.Get("X-Caller"))
	require.NoError(t, ApplyRequest(other, request))
	require.Equal(t, "Bearer later-human", request.Header.Get("Authorization"))
}

func TestInvocationSnapshotKeepsItsTokenAndBindings(t *testing.T) {
	bindings := []Header{{Name: "Authorization", Prefix: "Bearer "}}
	targets := []Target{{Origin: "https://tools.example:443", Passthrough: bindings}}
	ctx := invocationContext(t, "machine", targets)
	source, err := FromContext(ctx)
	require.NoError(t, err)
	next := invocationContext(t, "later-human", []Target{})
	request := httptest.NewRequest(http.MethodPost, "https://tools.example/mcp", nil)
	for range 3 {
		require.NoError(t, source.ApplyRequest(request))
		require.Equal(t, "Bearer machine", request.Header.Get("Authorization"))
	}
	request = httptest.NewRequest(http.MethodPost, "https://tools.example/mcp", nil)
	require.NoError(t, ApplyRequest(next, request))
	require.Empty(t, request.Header.Get("Authorization"))
}

func TestInvocationSnapshotIncludesPolicyOnlyHeaders(t *testing.T) {
	ctx := invocationContext(t, "caller", []Target{{Origin: "https://tools.example:443",
		Headers:     map[string]string{"X-Key": "kagent-credential-injected"},
		Passthrough: []Header{{Name: "X-Caller"}},
	}})
	request := httptest.NewRequest(http.MethodPost, "https://tools.example/mcp", nil)
	require.NoError(t, ApplyRequest(ctx, request))
	require.Equal(t, "caller", request.Header.Get("X-Caller"))
	require.Equal(t, "kagent-credential-injected", request.Header.Get("X-Key"))
}

func TestMalformedInvocationSnapshotNeverFallsBack(t *testing.T) {
	for _, values := range [][]string{nil, {"null"}, {"{}"}, {"invalid"}, {"[]", "[]"}} {
		ctx, _ := a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(map[string][]string{
			"Authorization": {"Bearer caller"}, HeadersMetadataKey: values,
		}))
		request := httptest.NewRequest(http.MethodPost, "https://tools.example/mcp", nil)
		err := ApplyRequest(ctx, request)
		require.ErrorContains(t, err, "invalid invocation header bindings")
		require.Empty(t, request.Header.Get("Authorization"))
	}
}

func TestMissingAndMalformedInvocationCredentials(t *testing.T) {
	for _, values := range [][]string{nil, {"Bearer "}, {"Bearer one", "Bearer two"}, {"Basic secret"}, {"Bearer kagent-credential-injected"}} {
		ctx, _ := a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(map[string][]string{"Authorization": values}))
		_, err := FromContext(ctx)
		require.Error(t, err)
	}
	ctx := invocationContext(t, "", []Target{{Origin: "https://tools.example:443", Passthrough: []Header{{Name: "Authorization"}}}})
	request := httptest.NewRequest(http.MethodPost, "https://tools.example/mcp", nil)
	require.ErrorContains(t, ApplyRequest(ctx, request), "credential unavailable")
	require.NoError(t, ApplyRequest(invocationContext(t, "", []Target{}), request), "Secret and Actor token requests need no caller")
}

func TestInvocationSnapshotCanonicalOrigins(t *testing.T) {
	bindings := []Header{{Name: "Authorization", Prefix: "Bearer "}}
	ctx := invocationContext(t, "caller", []Target{{Origin: "https://tools.example:443", Passthrough: bindings}})
	for _, endpoint := range []string{"https://TOOLS.EXAMPLE/mcp", "https://tools.example./mcp", "https://tools.example:0443/mcp", "https://tools.example.:443/mcp"} {
		t.Run(endpoint, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, endpoint, nil)
			require.NoError(t, ApplyRequest(ctx, request))
			require.Equal(t, "Bearer caller", request.Header.Get("Authorization"))
		})
	}
	for _, endpoint := range []string{"http://tools.example:443/mcp", "https://tools.example:8443/mcp", "https://other.example/mcp"} {
		request := httptest.NewRequest(http.MethodPost, endpoint, nil)
		require.NoError(t, ApplyRequest(ctx, request))
		require.Empty(t, request.Header.Get("Authorization"))
	}
}

func TestInvocationSnapshotAcceptsLargeAdmittedPolicies(t *testing.T) {
	var targets []Target
	for destination := range 16 {
		target := Target{Origin: fmt.Sprintf("https://tools-%d.example:443", destination)}
		for header := range 16 {
			target.Passthrough = append(target.Passthrough, Header{Name: fmt.Sprintf("X-Caller-%d", header), Prefix: strings.Repeat("a", 256)})
		}
		targets = append(targets, target)
	}
	data, err := json.Marshal(targets)
	require.NoError(t, err)
	require.Greater(t, len(data), 65536)
	ctx := invocationContext(t, "caller", targets)
	request := httptest.NewRequest(http.MethodPost, "https://tools-15.example/mcp", nil)
	require.NoError(t, ApplyRequest(ctx, request))
	require.Equal(t, strings.Repeat("a", 256)+"caller", request.Header.Get("X-Caller-15"))
}
