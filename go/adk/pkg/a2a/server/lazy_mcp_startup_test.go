package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"

	kagentagent "github.com/kagent-dev/kagent/go/adk/pkg/agent"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

func TestReadinessDoesNotWaitForMCPDiscovery(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Run("Given slow MCP servers and MCP-bound subagents, when the runtime starts, assert that readiness precedes discovery", func(t *testing.T) {
		var requests atomic.Int32
		mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			select {
			case <-time.After(500 * time.Millisecond):
			case <-r.Context().Done():
			}
			http.Error(w, "slow server", http.StatusServiceUnavailable)
		}))
		t.Cleanup(mcpServer.Close)
		marker := filepath.Join(t.TempDir(), "stdio-started")
		config := func(name string) *configv1.ADKAgentConfig {
			return &configv1.ADKAgentConfig{
				Name: name, Instruction: "Use the configured tools.", SkillsDirectory: t.TempDir(),
				Model:     &configv1.ADKModelConfig{Model: "test-model", Provider: &configv1.ADKModelConfig_Openai{Openai: &configv1.ADKOpenAI{}}},
				HttpTools: []*configv1.ADKHTTPMCPServerConfig{{Params: &configv1.ADKHTTPConnectionParams{Url: mcpServer.URL, Timeout: durationpb.New(time.Second)}}},
				SseTools:  []*configv1.ADKSSEMCPServerConfig{{Params: &configv1.ADKSSEConnectionParams{Url: mcpServer.URL, Timeout: durationpb.New(time.Second)}}},
			}
		}
		rootConfig := config("root")
		rootConfig.StdioTools = []*configv1.ADKStdioMCPServerConfig{{Command: "touch", Args: []string{marker}}}
		rootConfig.SubAgents = []*configv1.ADKAgentConfig{config("child")}
		_, err := kagentagent.CreateGoogleADKAgent(t.Context(), &configv1.ADKConfig{Agent: rootConfig}, "root", nil)
		require.NoError(t, err)
		srv, err := NewA2AServer(a2atype.AgentCard{}, substrateExecutor{}, slog.New(slog.DiscardHandler), ServerConfig{Host: "127.0.0.1", Port: "0"})
		require.NoError(t, err)
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		srv.readyServer.Addr = listener.Addr().String()
		require.NoError(t, listener.Close())
		require.NoError(t, srv.Start())
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.NoError(t, srv.httpServer.Shutdown(ctx))
			require.NoError(t, srv.readyServer.Shutdown(ctx))
			srv.grpcServer.Stop()
		})
		client := &http.Client{Timeout: time.Second}
		resp, err := client.Get("http://" + srv.readyServer.Addr + "/readyz")
		require.NoError(t, err)
		_, err = io.Copy(io.Discard, resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Zero(t, requests.Load(), "root and subagent MCP connections must be deferred")
		_, err = os.Stat(marker)
		require.ErrorIs(t, err, os.ErrNotExist, "stdio process must not start before readiness")
	})
}
