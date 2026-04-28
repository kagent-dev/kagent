package tools

import (
	"context"
	"reflect"
	"testing"

	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2asrv"
)

func TestSubagentForwardingInterceptorForwardsUserIDAndAuthorization(t *testing.T) {
	ctx := context.WithValue(context.Background(), userIDContextKey{}, "user-1")
	ctx = context.WithValue(ctx, authorizationHeaderContextKey{}, "Bearer parent-token")

	req := &a2aclient.Request{Meta: a2aclient.CallMeta{}}
	_, err := (&subagentForwardingInterceptor{}).Before(ctx, req)
	if err != nil {
		t.Fatalf("Before() error = %v", err)
	}

	if got, want := req.Meta.Get("x-user-id"), []string{"user-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("x-user-id = %v, want %v", got, want)
	}
	if got, want := req.Meta.Get("authorization"), []string{"Bearer parent-token"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("authorization = %v, want %v", got, want)
	}
}

func TestSubagentForwardingInterceptorReplacesExistingAuthorization(t *testing.T) {
	ctx := context.WithValue(context.Background(), authorizationHeaderContextKey{}, "Bearer parent-token")

	req := &a2aclient.Request{Meta: a2aclient.CallMeta{}}
	req.Meta.Append("Authorization", "Bearer stale-token")

	_, err := (&subagentForwardingInterceptor{}).Before(ctx, req)
	if err != nil {
		t.Fatalf("Before() error = %v", err)
	}

	if got, want := req.Meta.Get("authorization"), []string{"Bearer parent-token"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("authorization = %v, want %v", got, want)
	}
}

func TestAuthorizationHeaderFromContext(t *testing.T) {
	ctx, _ := a2asrv.WithCallContext(context.Background(), a2asrv.NewRequestMeta(map[string][]string{
		"Authorization": {"Bearer parent-token"},
		"X-Other":      {"ignored"},
	}))

	if got, want := authorizationHeaderFromContext(ctx), "Bearer parent-token"; got != want {
		t.Fatalf("authorizationHeaderFromContext() = %q, want %q", got, want)
	}
}

func TestAuthorizationHeaderFromContextWithoutHeader(t *testing.T) {
	ctx, _ := a2asrv.WithCallContext(context.Background(), a2asrv.NewRequestMeta(map[string][]string{
		"X-Other": {"ignored"},
	}))

	if got := authorizationHeaderFromContext(ctx); got != "" {
		t.Fatalf("authorizationHeaderFromContext() = %q, want empty", got)
	}
}
