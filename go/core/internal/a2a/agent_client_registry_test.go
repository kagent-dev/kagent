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
	var called atomic.Bool
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
					"parts":     []any{map[string]any{"text": "hello from sandbox"}},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	t.Cleanup(server.Close)

	client, err := a2aclient.NewFromEndpoints(
		context.Background(),
		[]*a2atype.AgentInterface{{
			URL:             server.URL,
			ProtocolBinding: a2atype.TransportProtocolJSONRPC,
			ProtocolVersion: a2atype.Version,
		}},
		a2aclient.WithJSONRPCTransport(&http.Client{}),
	)
	require.NoError(t, err)

	registry := NewAgentClientRegistry()
	sandboxGroupKind := schema.GroupKind{Group: "kagent.dev", Kind: "SandboxAgent"}.String()
	require.NoError(t, registry.RegisterForGroupKind(sandboxGroupKind, "default", "sandbox-agent", client))

	_, err = registry.SendMessageForGroupKind(
		context.Background(),
		sandboxGroupKind,
		"default",
		"sandbox-agent",
		&a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hello"))},
	)
	require.NoError(t, err)
	require.True(t, called.Load())

	_, err = registry.SendMessage(
		context.Background(),
		"default",
		"sandbox-agent",
		&a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hello"))},
	)
	require.Error(t, err)
}
