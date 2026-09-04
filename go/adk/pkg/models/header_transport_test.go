package models

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/adk/pkg/headers"
)

// a2aCtx builds a context that carries an A2A CallContext with the given
// headers, matching what the A2A server puts in the context for real requests.
func a2aCtx(headers map[string][]string) context.Context {
	ctx, _ := a2asrv.NewCallContext(context.Background(), a2asrv.NewServiceParams(headers))
	return ctx
}

// doRequest sends a GET through the client built from tc and returns the
// headers the server received.
func doRequest(t *testing.T, tc TransportConfig, ctx context.Context, mutate func(*http.Request)) http.Header {
	t.Helper()

	var captured http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client, err := BuildHTTPClient(tc)
	if err != nil {
		t.Fatalf("BuildHTTPClient() error = %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	if mutate != nil {
		mutate(req)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}
	resp.Body.Close()
	return captured
}

// TestHeaderTransport_Passthrough verifies that configured pass-through
// headers are resolved from the incoming A2A call context per request, that
// unconfigured headers are not forwarded, and that a pass-through value
// overrides a static defaultHeaders entry of the same name.
func TestHeaderTransport_Passthrough(t *testing.T) {
	t.Parallel()

	tc := TransportConfig{
		Headers:            map[string]string{"x-static": "static-value", "x-tenant": "config-tenant"},
		PassthroughHeaders: []string{"x-guardrail-token", "x-tenant", "x-missing"},
	}
	ctx := a2aCtx(map[string][]string{
		"X-Guardrail-Token": {"caller-token"},
		"X-Tenant":          {"caller-tenant"},
		"X-Ignored":         {"should-not-appear"},
	})

	got := doRequest(t, tc, ctx, nil)

	if v := got.Get("x-guardrail-token"); v != "caller-token" {
		t.Errorf("x-guardrail-token = %q, want %q", v, "caller-token")
	}
	if v := got.Get("x-static"); v != "static-value" {
		t.Errorf("x-static = %q, want %q", v, "static-value")
	}
	if v := got.Get("x-tenant"); v != "caller-tenant" {
		t.Errorf("x-tenant = %q, want pass-through value %q to override the static one", v, "caller-tenant")
	}
	if v := got.Get("x-ignored"); v != "" {
		t.Errorf("x-ignored should not be forwarded, got %q", v)
	}
	if got.Values("x-missing") != nil {
		t.Errorf("x-missing absent from the request should be omitted, got %q", got.Values("x-missing"))
	}
}

// TestHeaderTransport_PassthroughNeverClobbersCredentials verifies that
// restricted names (Authorization, hop-by-hop headers) are stripped from the
// pass-through list, so a caller can never override the provider credential
// the SDK set on the request.
func TestHeaderTransport_PassthroughNeverClobbersCredentials(t *testing.T) {
	t.Parallel()

	tc := TransportConfig{
		PassthroughHeaders: []string{"Authorization", "Connection", "x-allowed"},
	}
	ctx := a2aCtx(map[string][]string{
		"Authorization": {"Bearer caller-token"},
		"Connection":    {"close"},
		"X-Allowed":     {"ok"},
	})

	got := doRequest(t, tc, ctx, func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer provider-credential")
	})

	if v := got.Get("Authorization"); v != "Bearer provider-credential" {
		t.Errorf("Authorization = %q, want the provider credential to be preserved", v)
	}
	if v := got.Get("x-allowed"); v != "ok" {
		t.Errorf("x-allowed = %q, want %q", v, "ok")
	}
}

// TestHeaderTransport_NoCallContext verifies that requests without an A2A
// call context still go out with the static headers and nothing else.
func TestHeaderTransport_NoCallContext(t *testing.T) {
	t.Parallel()

	tc := TransportConfig{
		Headers:            map[string]string{"x-static": "static-value"},
		PassthroughHeaders: []string{"x-guardrail-token"},
	}

	got := doRequest(t, tc, context.Background(), nil)

	if v := got.Get("x-static"); v != "static-value" {
		t.Errorf("x-static = %q, want %q", v, "static-value")
	}
	if v := got.Get("x-guardrail-token"); v != "" {
		t.Errorf("x-guardrail-token should be absent without a call context, got %q", v)
	}
}

func TestFilterRestricted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		names []string
		want  []string
	}{
		{name: "nil list", names: nil, want: nil},
		{name: "plain names pass", names: []string{"x-a", "X-B"}, want: []string{"x-a", "X-B"}},
		{name: "authorization dropped case-insensitively", names: []string{"Authorization", "authorization", "AUTHORIZATION", "x-ok"}, want: []string{"x-ok"}},
		{name: "credential headers dropped", names: []string{"Proxy-Authorization", "Cookie", "x-ok"}, want: []string{"x-ok"}},
		{name: "hop-by-hop dropped", names: []string{"Connection", "Transfer-Encoding", "Host", "Content-Length", "Proxy-Authenticate", "x-ok"}, want: []string{"x-ok"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := headers.FilterRestricted(tt.names)
			if len(got) != len(tt.want) {
				t.Fatalf("headers.FilterRestricted() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("headers.FilterRestricted()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
