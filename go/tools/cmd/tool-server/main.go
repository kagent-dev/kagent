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

	"github.com/kagent-dev/kagent/go/tools/internal/argo"
	"github.com/kagent-dev/kagent/go/tools/internal/cilium"
	"github.com/kagent-dev/kagent/go/tools/internal/common"
	"github.com/kagent-dev/kagent/go/tools/internal/datetime"
	"github.com/kagent-dev/kagent/go/tools/internal/grafana"
	"github.com/kagent-dev/kagent/go/tools/internal/helm"
	"github.com/kagent-dev/kagent/go/tools/internal/istio"
	"github.com/kagent-dev/kagent/go/tools/internal/k8s"
	"github.com/kagent-dev/kagent/go/tools/internal/prometheus"
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

	mcp := server.NewMCPServer(
		"kagent-tools",
		"1.0.0",
	)

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	// Register tools
	registerMCP(mcp, tools)

	if stdio {
		logger.Get().Info("Running KAgent Tools Server STDIO:", "tools", strings.Join(tools, ","))
		stdioServer := server.NewStdioServer(mcp)
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
		sseServer := server.NewSSEServer(mcp)

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

func registerMCP(mcp *server.MCPServer, enabledToolProviders []string) {

	var toolProviderMap = map[string]func(*server.MCPServer){
		"common":     common.RegisterCommonTools,
		"k8s":        k8s.RegisterK8sTools,
		"datetime":   datetime.RegisterDateTimeTools,
		"prometheus": prometheus.RegisterPrometheusTools,
		"helm":       helm.RegisterHelmTools,
		"istio":      istio.RegisterIstioTools,
		"argo":       argo.RegisterArgoTools,
		"cilium":     cilium.RegisterCiliumTools,
		"grafana":    grafana.RegisterGrafanaTools,
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
