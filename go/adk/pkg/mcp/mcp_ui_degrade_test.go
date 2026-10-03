package mcp

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/v2/tool"

	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// startWeatherMCPServer serves a single getWeather tool on a fresh port and
// returns its URL plus a function that shuts it down.
func startWeatherMCPServer(t *testing.T) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	mcpServer := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "weather-test", Version: "1.0.0"}, nil)
	mcpsdk.AddTool(mcpServer, &mcpsdk.Tool{Name: "getWeather"}, func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, map[string]any, error) {
		return nil, map[string]any{"weather": "sunny"}, nil
	})
	httpServer := &http.Server{Handler: mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server {
		return mcpServer
	}, nil)}
	go func() { _ = httpServer.Serve(listener) }()
	stop := func() { _ = httpServer.Close() }
	t.Cleanup(stop)
	return "http://" + listener.Addr().String(), stop
}

// loggingContext returns a ReadonlyContext whose logger writes JSON to buf.
func loggingContext(t *testing.T, buf *bytes.Buffer) testReadonlyContext {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(buf, nil))
	return testReadonlyContext{Context: logging.IntoContext(t.Context(), logger)}
}

func toolNames(tools []tool.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tl := range tools {
		names = append(names, tl.Name())
	}
	return names
}

func TestMCPAppToolsetServesLastKnownToolsWhenServerDies(t *testing.T) {
	serverURL, stop := startWeatherMCPServer(t)

	toolset, err := initializeToolSet(t.Context(), mcpServerParams{URL: serverURL, ServerType: "http"}, nil)
	if err != nil {
		t.Fatalf("initializeToolSet() error = %v", err)
	}

	var logs bytes.Buffer
	ctx := loggingContext(t, &logs)

	tools, err := toolset.Tools(ctx)
	if err != nil {
		t.Fatalf("Tools() with live server error = %v", err)
	}
	if got := toolNames(tools); len(got) != 1 || got[0] != "getWeather" {
		t.Fatalf("Tools() with live server = %v, want [getWeather]", got)
	}
	if logs.Len() != 0 {
		t.Fatalf("unexpected log output with live server: %s", logs.String())
	}

	stop()

	tools, err = toolset.Tools(ctx)
	if err != nil {
		t.Fatalf("Tools() after server died error = %v, want degraded success", err)
	}
	if got := toolNames(tools); len(got) != 1 || got[0] != "getWeather" {
		t.Fatalf("Tools() after server died = %v, want last known [getWeather]", got)
	}
	out := logs.String()
	if !strings.Contains(out, `"level":"ERROR"`) || !strings.Contains(out, serverURL) || !strings.Contains(out, "last known tool list") {
		t.Fatalf("expected an error-level log naming %s and the degraded mode, got: %s", serverURL, out)
	}
}

func TestMCPAppToolsetReturnsNoToolsWhenServerNeverReachable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	serverURL := "http://" + listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("listener.Close() error = %v", err)
	}

	toolset, err := initializeToolSet(t.Context(), mcpServerParams{URL: serverURL, ServerType: "http"}, nil)
	if err != nil {
		t.Fatalf("initializeToolSet() error = %v, want lazy fallback", err)
	}

	var logs bytes.Buffer
	tools, err := toolset.Tools(loggingContext(t, &logs))
	if err != nil {
		t.Fatalf("Tools() against dead server error = %v, want degraded success", err)
	}
	if len(tools) != 0 {
		t.Fatalf("Tools() against dead server = %v, want none", toolNames(tools))
	}
	out := logs.String()
	if !strings.Contains(out, `"level":"ERROR"`) || !strings.Contains(out, serverURL) || !strings.Contains(out, "never listed") {
		t.Fatalf("expected an error-level log naming %s, got: %s", serverURL, out)
	}
}

func TestMCPAppToolsetPropagatesCancelledContext(t *testing.T) {
	serverURL, _ := startWeatherMCPServer(t)
	toolset, err := initializeToolSet(t.Context(), mcpServerParams{URL: serverURL, ServerType: "http"}, nil)
	if err != nil {
		t.Fatalf("initializeToolSet() error = %v", err)
	}

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := toolset.Tools(testReadonlyContext{Context: cancelled}); err == nil {
		t.Fatal("Tools() with cancelled context error = nil, want the cancellation surfaced")
	}
}
