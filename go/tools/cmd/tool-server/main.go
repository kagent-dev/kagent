package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/kagent-dev/kagent/go/tools/pkg/utils"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/kagent-dev/kagent/go/internal/version"
	"github.com/kagent-dev/kagent/go/tools/pkg/logger"

	"github.com/kagent-dev/kagent/go/tools/pkg/argo"
	"github.com/kagent-dev/kagent/go/tools/pkg/cilium"
	"github.com/kagent-dev/kagent/go/tools/pkg/helm"
	"github.com/kagent-dev/kagent/go/tools/pkg/istio"
	"github.com/kagent-dev/kagent/go/tools/pkg/k8s"
	"github.com/kagent-dev/kagent/go/tools/pkg/prometheus"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

var (
	port  int
	stdio bool
	tools []string

	// These variables should be set during build time using -ldflags
	Name      = "kagent-tools-server"
	Version   = version.Version
	GitCommit = version.GitCommit
	BuildDate = version.BuildDate
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

	logger.Get().Info("Starting "+Name, "version", Version, "git_commit", GitCommit, "build_date", BuildDate)

	// Setup context with cancellation for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mcp := server.NewMCPServer(
		Name,
		Version,
	)

	// Register tools
	registerMCP(mcp, tools)

	// Create wait group for server goroutines
	var wg sync.WaitGroup

	// Setup signal handling
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	// HTTP server reference (only used when not in stdio mode)
	var sseServer *server.SSEServer

	// Start server based on chosen mode
	wg.Add(1)
	if stdio {
		go func() {
			defer wg.Done()
			runStdioServer(ctx, mcp)
		}()
	} else {
		sseServer = server.NewSSEServer(mcp)
		go func() {
			defer wg.Done()
			addr := fmt.Sprintf(":%d", port)
			logger.Get().Info("Running KAgent Tools Server", "port", addr, "tools", strings.Join(tools, ","))
			if err := sseServer.Start(addr); err != nil {
				if !errors.Is(err, http.ErrServerClosed) {
					logger.Get().Error(err, "Failed to start SSE server")
				} else {
					logger.Get().Info("SSE server closed gracefully.")
				}
			}
		}()
	}

	// Wait for termination signal
	go func() {
		<-signalChan
		logger.Get().Info("Received termination signal, shutting down server...")

		// Cancel context to notify any context-aware operations
		cancel()

		// Gracefully shutdown HTTP server if running
		if !stdio && sseServer != nil {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()

			if err := sseServer.Shutdown(shutdownCtx); err != nil {
				logger.Get().Error(err, "Failed to shutdown server gracefully")
			}
		}
	}()

	// Wait for all server operations to complete
	wg.Wait()
	logger.Get().Info("Server shutdown complete")
}

func runStdioServer(ctx context.Context, mcp *server.MCPServer) {
	logger.Get().Info("Running KAgent Tools Server STDIO:", "tools", strings.Join(tools, ","))
	stdioServer := server.NewStdioServer(mcp)
	if err := stdioServer.Listen(ctx, os.Stdin, os.Stdout); err != nil {
		logger.Get().Info("Stdio server stopped", "error", err)
	}
}

func registerMCP(mcp *server.MCPServer, enabledToolProviders []string) {

	var toolProviderMap = map[string]func(*server.MCPServer){
		"utils":      utils.RegisterDateTimeTools,
		"k8s":        k8s.RegisterK8sTools,
		"prometheus": prometheus.RegisterPrometheusTools,
		"helm":       helm.RegisterHelmTools,
		"istio":      istio.RegisterIstioTools,
		"argo":       argo.RegisterArgoTools,
		"cilium":     cilium.RegisterCiliumTools,
	}

	// If no tools specified, register all tools
	if len(enabledToolProviders) == 0 {
		logger.Get().Info("No specific tools provided, registering all tools")
		for toolProvider, registerFunc := range toolProviderMap {
			logger.Get().Info("Registering tools", "provider", toolProvider)
			registerFunc(mcp)
		}
		return
	}

	// Register only the specified tools
	logger.Get().Info("provider list", "tools", enabledToolProviders)
	for _, toolProviderName := range enabledToolProviders {
		if registerFunc, ok := toolProviderMap[strings.ToLower(toolProviderName)]; ok {
			logger.Get().Info("Registering tool", "provider", toolProviderName)
			registerFunc(mcp)
		} else {
			logger.Get().Error(nil, "Unknown tool specified", "provider", toolProviderName)
		}
	}
}
