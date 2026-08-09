package a2a

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2aclient "github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestAgentClientRegistrySendMessageRoutesByGroupKind(t *testing.T) {
	var regularCalled, sandboxCalled atomic.Bool
	regularServer := newA2ATestServer(t, &regularCalled, "hello from regular agent")
	sandboxServer := newA2ATestServer(t, &sandboxCalled, "hello from sandbox agent")

	registry := NewAgentClientRegistry()
	const namespace = "default"
	const name = "shared-agent"
	agentGroupKind := schema.GroupKind{Group: "kagent.dev", Kind: "Agent"}.String()
	sandboxGroupKind := schema.GroupKind{Group: "kagent.dev", Kind: "SandboxAgent"}.String()
	registry.set(routeKey(false, namespace, name), newA2ATestClient(t, regularServer.URL))
	registry.set(routeKey(true, namespace, name), newA2ATestClient(t, sandboxServer.URL))

	_, err := registry.SendMessage(
		context.Background(),
		agentGroupKind,
		namespace,
		name,
		&a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hello"))},
	)
	require.NoError(t, err)
	require.True(t, regularCalled.Load())
	require.False(t, sandboxCalled.Load())

	regularCalled.Store(false)
	_, err = registry.SendMessage(
		context.Background(),
		sandboxGroupKind,
		namespace,
		name,
		&a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hello"))},
	)
	require.NoError(t, err)
	require.False(t, regularCalled.Load())
	require.True(t, sandboxCalled.Load())
}

func newA2ATestServer(t *testing.T, called *atomic.Bool, responseText string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)
		var rpcReq map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&rpcReq))
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      rpcReq["id"],
			"result": map[string]any{
				"message": map[string]any{
					"messageId": "test-msg",
					"role":      "ROLE_AGENT",
					"parts":     []any{map[string]any{"text": responseText}},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	t.Cleanup(server.Close)
	return server
}

func newA2ATestClient(t *testing.T, endpoint string) *a2aclient.Client {
	t.Helper()
	client, err := a2aclient.NewFromEndpoints(
		context.Background(),
		[]*a2atype.AgentInterface{{
			URL:             endpoint,
			ProtocolBinding: a2atype.TransportProtocolJSONRPC,
			ProtocolVersion: a2atype.Version,
		}},
		a2aclient.WithJSONRPCTransport(&http.Client{}),
	)
	require.NoError(t, err)
	return client
}
