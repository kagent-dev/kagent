package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/kagent-dev/kagent/go/tools/internal/logger"
	"github.com/kagent-dev/kagent/go/tools/internal/telemetry"

	"github.com/kagent-dev/kagent/go/tools/internal/argo"
	"github.com/kagent-dev/kagent/go/tools/internal/cilium"
	"github.com/kagent-dev/kagent/go/tools/internal/common"
	"github.com/kagent-dev/kagent/go/tools/internal/datetime"
	"github.com/kagent-dev/kagent/go/tools/internal/grafana"
	"github.com/kagent-dev/kagent/go/tools/internal/helm"
	"github.com/kagent-dev/kagent/go/tools/internal/istio"
	"github.com/kagent-dev/kagent/go/tools/internal/k8s"
	"github.com/kagent-dev/kagent/go/tools/internal/prometheus"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

var (
	port  int
	stdio bool
	tools []string
)

var rootCmd = &cobra.Command{
	Use:   "tool-server",
	Short: "KAgent tool server",
	Run:   run,
}

func init() {
	rootCmd.Flags().IntVarP(&port, "port", "p", 8084, "Port to run the server on")
	rootCmd.Flags().BoolVar(&stdio, "stdio", false, "Use stdio for communication instead of HTTP")
	rootCmd.Flags().StringSliceVar(&tools, "tools", []string{}, "List of tools to register. If empty, all tools are registered.")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) {
	logger.Init()
	defer logger.Sync()

	// Initialize OpenTelemetry if enabled
	ctx := context.Background()
	shutdown, err := telemetry.InitTelemetry(ctx)
	if err != nil {
		logger.Get().Error(err, "Failed to initialize telemetry")
	} else {
		defer shutdown()
		logger.Get().Info("OpenTelemetry initialized successfully")
	}

	// Create instrumented MCP server
	instrumentedMCP := telemetry.NewInstrumentedMCPServer(
		"kagent-tools",
		"1.0.0",
	)

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	// Register tools - the instrumented server implements the same interface
	// as the regular MCP server, so we can pass it directly to the register functions
	registerMCP(instrumentedMCP, tools)

	if stdio {
		logger.Get().Info("Running KAgent Tools Server STDIO:", "tools", strings.Join(tools, ","))
		stdioServer := server.NewStdioServer(instrumentedMCP.MCPServer)
		go func() {
			if err := stdioServer.Listen(context.Background(), os.Stdin, os.Stdout); err != nil {
				logger.Get().Info("Stdio server stopped", "error", err)
			}
		}()

		<-signalChan
		logger.Get().Info("Shutting down...")
		// For stdio, closing stdin or terminating the process is enough.

	} else {
		addr := fmt.Sprintf(":%d", port)
		logger.Get().Info("Running KAgent Tools Server", "port", addr, "tools", strings.Join(tools, ","))
		sseServer := server.NewSSEServer(instrumentedMCP.MCPServer)

		go func() {
			if err := sseServer.Start(addr); err != nil {
				if !errors.Is(err, http.ErrServerClosed) {
					logger.Get().Error(err, "Failed to start SSE server")
				} else {
					logger.Get().Info("SSE server closed gracefully.")
				}
			}
		}()

		<-signalChan
		logger.Get().Info("Shutting down server...")
		if err := sseServer.Shutdown(context.Background()); err != nil {
			logger.Get().Error(err, "Failed to shutdown server gracefully")
		}
	}
}

// MCPServerInterface defines the interface that both regular and instrumented MCP servers implement
type MCPServerInterface interface {
	AddTool(tool mcp.Tool, handler server.ToolHandlerFunc)
}

func registerMCP(mcpServer MCPServerInterface, enabledToolProviders []string) {

	var toolProviderMap = map[string]func(MCPServerInterface){
		"common": func(s MCPServerInterface) { common.RegisterCommonTools(s.(*telemetry.InstrumentedMCPServer).MCPServer) },
		"k8s":    func(s MCPServerInterface) { k8s.RegisterK8sTools(s.(*telemetry.InstrumentedMCPServer).MCPServer) },
		"datetime": func(s MCPServerInterface) {
			datetime.RegisterDateTimeTools(s.(*telemetry.InstrumentedMCPServer).MCPServer)
		},
		"prometheus": func(s MCPServerInterface) {
			prometheus.RegisterPrometheusTools(s.(*telemetry.InstrumentedMCPServer).MCPServer)
		},
		"helm":   func(s MCPServerInterface) { helm.RegisterHelmTools(s.(*telemetry.InstrumentedMCPServer).MCPServer) },
		"istio":  func(s MCPServerInterface) { istio.RegisterIstioTools(s.(*telemetry.InstrumentedMCPServer).MCPServer) },
		"argo":   func(s MCPServerInterface) { argo.RegisterArgoTools(s.(*telemetry.InstrumentedMCPServer).MCPServer) },
		"cilium": func(s MCPServerInterface) { cilium.RegisterCiliumTools(s.(*telemetry.InstrumentedMCPServer).MCPServer) },
		"grafana": func(s MCPServerInterface) {
			grafana.RegisterGrafanaTools(s.(*telemetry.InstrumentedMCPServer).MCPServer)
		},
	}

	// If no tools specified, register all tools
	if len(enabledToolProviders) == 0 {
		logger.Get().Info("No specific tools provided, registering all tools")
		for toolProvider, registerFunc := range toolProviderMap {
			logger.Get().Info("Registering tools", "provider", toolProvider)
			registerFunc(mcpServer)
		}
		return
	}

	// Register only the specified tools
	logger.Get().Info("provider list", "tools", enabledToolProviders)
	for _, toolProviderName := range enabledToolProviders {
		if registerFunc, ok := toolProviderMap[strings.ToLower(toolProviderName)]; ok {
			logger.Get().Info("Registering tool", "provider", toolProviderName)
			registerFunc(mcpServer)
		} else {
			logger.Get().Error(nil, "Unknown tool specified", "provider", toolProviderName)
		}
	}
}
