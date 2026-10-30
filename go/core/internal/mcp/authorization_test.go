// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/jwttest"
	"github.com/kagent-dev/kagent/go/core/internal/runtimeauth"
	sessionsvc "github.com/kagent-dev/kagent/go/core/internal/service/session"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type mcpActorPolicy struct {
	auth.NoopAuthorizer
	revoked atomic.Bool
}

var _ auth.CollectionAuthorizer = (*mcpActorPolicy)(nil)

func (p *mcpActorPolicy) Check(_ context.Context, principal auth.Principal, _ auth.Verb, _ auth.Resource) error {
	if principal.RuntimeBinding != nil && p.revoked.Load() {
		return errors.New("actor permission revoked")
	}
	return nil
}

type mcpAuthTransport struct {
	http.RoundTripper
	headers http.Header
}

var _ http.RoundTripper = mcpAuthTransport{}

func (t mcpAuthTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	for name, values := range t.headers {
		request.Header.Del(name)
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	return t.RoundTripper.RoundTrip(request)
}

func TestMCPActorObservesConfiguredPolicyRevocation(t *testing.T) {
	own := &apiv1.Session{
		Id: testSessionID, ContextId: testSessionID, Creator: "owner",
		Agent:        &apiv1.ResourceReference{Namespace: "team-a", Name: "assistant"},
		State:        apiv1.RuntimeState_RUNTIME_STATE_READY,
		A2AAuthority: substrate.ActorHost("team-a", substrate.ActorName(testSessionID), ""),
	}
	store := &actorSessionStore{fakeSessionStore: &fakeSessionStore{}, sessions: []*apiv1.Session{own}}
	issuer := jwttest.New(t)
	verifier, err := substrate.NewActorJWTVerifier(t.Context(), issuer.URL, issuer.CAFile)
	require.NoError(t, err)
	provider := runtimeauth.NewProvider(&authimpl.InsecureAuthenticator{}, verifier, store, issuer.URL)
	configured := &mcpActorPolicy{}
	sessions := sessionsvc.NewService(store, runtimeauth.NewAuthorizer(configured, store), &fakeSessionWorkflow{})
	gateway := &fakeGateway{}
	handler, err := New(sessions, testCheckpointService(), gateway, nil, nil)
	require.NoError(t, err)
	server := httptest.NewServer(auth.AuthnMiddleware(provider)(handler))
	t.Cleanup(server.Close)
	connect := func(headers http.Header) *mcp.ClientSession {
		t.Helper()
		client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
		session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
			Endpoint: server.URL,
			HTTPClient: &http.Client{Transport: mcpAuthTransport{
				RoundTripper: http.DefaultTransport, headers: headers,
			}},
		}, &mcp.ClientSessionOptions{ProtocolVersion: "2026-07-28"})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, session.Close()) })
		require.Equal(t, "2026-07-28", session.InitializeResult().ProtocolVersion)
		return session
	}
	token := "Bearer " + issuer.Token(t, issuer.Claims("team-a", substrate.ActorName(own.Id), "actor-uid"))
	actor := connect(http.Header{"Authorization": {token}, "X-User-Id": {own.Creator}})
	creator := connect(http.Header{"X-User-Id": {own.Creator}})
	tools := []*mcp.CallToolParams{
		{Name: listToolName, Arguments: ListSessionsInput{}},
		{Name: invokeToolName, Arguments: InvokeSessionInput{SessionID: own.Id, Message: "hello"}},
	}
	call := func(client *mcp.ClientSession, params *mcp.CallToolParams) *mcp.CallToolResult {
		t.Helper()
		result, err := client.CallTool(t.Context(), params)
		require.NoError(t, err)
		return result
	}

	for _, params := range tools {
		require.False(t, call(actor, params).IsError)
	}
	gateway.mu.Lock()
	accepted := gateway.task
	gateway.mu.Unlock()
	require.NotNil(t, accepted)

	configured.revoked.Store(true)
	for _, params := range tools {
		require.True(t, call(actor, params).IsError, "the same signed Actor credential must observe revocation")
	}
	gateway.mu.Lock()
	afterDenied := gateway.task
	gateway.mu.Unlock()
	require.Same(t, accepted, afterDenied, "denied invocation must not reach the gateway")
	for _, params := range tools {
		require.False(t, call(creator, params).IsError, "creator and Actor grants are independent")
	}

	configured.revoked.Store(false)
	for _, params := range tools {
		require.False(t, call(actor, params).IsError)
	}
}
