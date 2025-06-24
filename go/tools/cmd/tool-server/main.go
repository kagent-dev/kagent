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

	"github.com/kagent-dev/kagent/go/tools/internal/argo"
	"github.com/kagent-dev/kagent/go/tools/internal/cilium"
	"github.com/kagent-dev/kagent/go/tools/internal/common"
	"github.com/kagent-dev/kagent/go/tools/internal/datetime"
	"github.com/kagent-dev/kagent/go/tools/internal/grafana"
	"github.com/kagent-dev/kagent/go/tools/internal/helm"
	"github.com/kagent-dev/kagent/go/tools/internal/istio"
	"github.com/kagent-dev/kagent/go/tools/internal/k8s"
	"github.com/kagent-dev/kagent/go/tools/internal/logger"
	"github.com/kagent-dev/kagent/go/tools/internal/prometheus"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
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

var toolMap = map[string]func(*server.MCPServer){
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

func init() {
	availableTools := []string{}
	for tool := range toolMap {
		availableTools = append(tools, tool)
	}

	rootCmd.Flags().IntVarP(&port, "port", "p", 8084, "Port to run the server on")
	rootCmd.Flags().BoolVar(&stdio, "stdio", false, "Use stdio for communication instead of HTTP")
	rootCmd.Flags().StringSliceVar(&tools, "tools", availableTools, "List of tools to register. If empty, all tools are registered.")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) {
	logger.Init()
	log := logger.Get()
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
		log.Info("Running KAgent Tools Server STDIO: ", zap.String("tools", strings.Join(tools, ",")))
		stdioServer := server.NewStdioServer(mcp)
		go func() {
			if err := stdioServer.Listen(context.Background(), os.Stdin, os.Stdout); err != nil {
				log.Info("Stdio server stopped", zap.Error(err))
			}
		}()

		<-signalChan
		log.Info("Shutting down...")
		// For stdio, closing stdin or terminating the process is enough.

	} else {
		addr := fmt.Sprintf(":%d", port)
		log.Info("Running KAgent Tools Server", zap.String("port", addr))
		sseServer := server.NewSSEServer(mcp)

		go func() {
			if err := sseServer.Start(addr); err != nil {
				if !errors.Is(err, http.ErrServerClosed) {
					log.Error("Failed to start SSE server", zap.Error(err))
				} else {
					log.Info("SSE server closed gracefully.")
				}
			}
		}()

		<-signalChan
		log.Info("Shutting down server...")
		if err := sseServer.Shutdown(context.Background()); err != nil {
			log.Error("Failed to shutdown server gracefully", zap.Error(err))
		}
	}
}

func registerMCP(mcp *server.MCPServer, enabledTools []string) {
	for _, toolName := range enabledTools {
		if registerFunc, ok := toolMap[strings.ToLower(toolName)]; ok {
			registerFunc(mcp)
		} else {
			logger.Get().Warn("Unknown tool specified", zap.String("tool", toolName))
		}
	}
}
