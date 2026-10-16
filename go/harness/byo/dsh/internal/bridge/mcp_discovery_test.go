package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/harness/byo/dsh/internal/config"
)

func TestMCPDiscoveryRejectsInvalidCatalogAndCursorCycle(t *testing.T) {
	for _, mode := range []string{"invalid-schema", "list-error", "cursor-cycle"} {
		t.Run(mode, func(t *testing.T) {
			var deletes, pages atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodDelete {
					deletes.Add(1)
					w.WriteHeader(200)
					return
				}
				if r.Method != http.MethodPost {
					w.WriteHeader(405)
					return
				}
				var request message
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					http.Error(w, "bad request", 400)
					return
				}
				if len(request.ID) == 0 {
					w.WriteHeader(202)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				var result any
				switch request.Method {
				case "initialize":
					w.Header().Set("Mcp-Session-Id", "audit-session")
					result = json.RawMessage(`{"protocolVersion":"2025-11-25","capabilities":{"tools":{}},"serverInfo":{"name":"audit","version":"1"}}`)
				case "tools/list":
					n := pages.Add(1)
					switch mode {
					case "invalid-schema":
						result = json.RawMessage(`{"tools":[{"name":"broken","inputSchema":{"type":"array"}}]}`)
					case "list-error":
						_ = json.NewEncoder(w).Encode(message{JSONRPC: "2.0", ID: request.ID, Error: &rpcError{Code: -32603, Message: "discovery unavailable"}})
						return
					case "cursor-cycle":
						cursor := "A"
						if n%2 == 0 {
							cursor = "B"
						}
						result = struct {
							Tools      []any  `json:"tools"`
							NextCursor string `json:"nextCursor"`
						}{Tools: []any{}, NextCursor: cursor}
					}
				default:
					http.Error(w, "unknown method", 400)
					return
				}
				data, _ := json.Marshal(result)
				_ = json.NewEncoder(w).Encode(message{JSONRPC: "2.0", ID: request.ID, Result: data})
			}))
			defer server.Close()
			cfg, err := config.Parse([]byte(fmt.Sprintf(`{"model":{"type":"openai","model":"mock"},"runtime":{"control_plane_url":"http://controller:8083"},"http_tools":[{"params":{"url":%q,"timeout":0.5,"terminate_on_close":true}}]}`, server.URL)))
			require.NoError(t, err)
			bindings, err := cfg.Bindings(nil)
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			start := time.Now()
			var connectErr error
			require.NotPanics(t, func() {
				proxy, err := startMCP(ctx, cfg, bindings)
				connectErr = err
				if proxy != nil {
					require.NoError(t, proxy.close())
				}
			}, "remote catalogs must fail as errors, without terminating the harness")
			require.Error(t, connectErr)
			t.Logf("mode=%s pages=%d deletes=%d duration=%s error=%v", mode, pages.Load(), deletes.Load(), time.Since(start), connectErr)
			require.Equal(t, int32(1), deletes.Load(), "failed discovery must terminate the established remote session")
			if mode == "cursor-cycle" {
				require.LessOrEqual(t, pages.Load(), int32(3), "a repeated cursor must fail before the timeout")
			}
		})
	}
}
