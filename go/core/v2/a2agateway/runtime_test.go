package a2agateway

import (
	"context"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// TestCallerHeadersInterceptor verifies that caller-supplied custom headers on
// the public gateway request are relayed to the runtime call, while
// credential, hop-by-hop, transport, and gRPC system metadata are not.
func TestCallerHeadersInterceptor(t *testing.T) {
	ctx, _ := a2asrv.NewCallContext(context.Background(), a2asrv.NewServiceParams(map[string][]string{
		"x-guardrail-token": {"caller-token"},
		"X-User-Email":      {"user@example.com"},
		"Authorization":     {"Bearer caller"},
		"Cookie":            {"session=abc"},
		"content-type":      {"application/grpc"},
		"grpc-timeout":      {"5S"},
		":authority":        {"gateway"},
		"traceparent":       {"00-abc-def-01"},
		"x-empty":           {""},
	}))
	req := &a2aclient.Request{ServiceParams: a2aclient.ServiceParams{}}

	if _, _, err := (&callerHeadersInterceptor{}).Before(ctx, req); err != nil {
		t.Fatalf("Before() error = %v", err)
	}

	for key, want := range map[string]string{
		"x-guardrail-token": "caller-token",
		"x-user-email":      "user@example.com",
	} {
		if got := req.ServiceParams.Get(key); len(got) != 1 || got[0] != want {
			t.Errorf("%s = %v, want [%s]", key, got, want)
		}
	}
	for _, key := range []string{
		"Authorization", "Cookie", "content-type", "grpc-timeout", ":authority", "traceparent", "x-empty",
	} {
		if got := req.ServiceParams.Get(key); len(got) != 0 {
			t.Errorf("%s must not be forwarded, got %v", key, got)
		}
	}
}

// TestCallerHeadersInterceptor_NoCallContext verifies the interceptor is a
// no-op for calls without a public call context (e.g. internal invocations).
func TestCallerHeadersInterceptor_NoCallContext(t *testing.T) {
	req := &a2aclient.Request{ServiceParams: a2aclient.ServiceParams{}}
	if _, _, err := (&callerHeadersInterceptor{}).Before(context.Background(), req); err != nil {
		t.Fatalf("Before() error = %v", err)
	}
	if got := req.ServiceParams.Get("x-guardrail-token"); len(got) != 0 {
		t.Errorf("expected no forwarded params, got %v", got)
	}
}
