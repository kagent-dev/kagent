package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/kagent-dev/kagent/go/tools/internal/argo"
	"github.com/kagent-dev/kagent/go/tools/internal/cilium"
	"github.com/kagent-dev/kagent/go/tools/internal/common"
	"github.com/kagent-dev/kagent/go/tools/internal/datetime"
	"github.com/kagent-dev/kagent/go/tools/internal/docs"
	"github.com/kagent-dev/kagent/go/tools/internal/grafana"
	"github.com/kagent-dev/kagent/go/tools/internal/helm"
	"github.com/kagent-dev/kagent/go/tools/internal/istio"
	"github.com/kagent-dev/kagent/go/tools/internal/k8s"
	"github.com/kagent-dev/kagent/go/tools/internal/prometheus"
	"github.com/mark3labs/mcp-go/server"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

var mutex sync.Mutex
var serverInstance *server.SSEServer
var e error

func main() {
	port := os.Getenv("KAGENT_TOOLS_PORT")
	if port == "" {
		port = ":8080" // Default port if not set
	}

	// Create MCP server
	StartToolsServer(port)
	fmt.Print("KAgent Tools Server started on :" + port + "\n")

	// Wait for shutdown signal
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	StopToolsServer()
}

func StartToolsServer(addr string) {
	serverInstance, e = RunSSEServer(addr)
	if e != nil {
		if errors.Is(e, http.ErrServerClosed) {
			// Server was closed gracefully, no need to panic
			fmt.Println("Tools server closed gracefully")
			return
		}
		fmt.Printf("Failed to start tools server: %v\n", e)
		panic(e)
	}
}

func StopToolsServer() {
	if serverInstance != nil {
		if err := serverInstance.Shutdown(context.Background()); err != nil {
			panic(err)
		}
		serverInstance = nil
	}
}

func RunSSEServer(addr string) (*server.SSEServer, error) {
	mcp := server.NewMCPServer(
		"kagent-tools",
		"1.0.0",
	)
	RegisterMCP(mcp)
	srv := server.NewSSEServer(mcp)
	return srv, srv.Start(addr)
}

func RegisterMCP(mcp *server.MCPServer) {
	// Register tools
	k8s.RegisterK8sTools(mcp)
	common.RegisterCommonTools(mcp)
	datetime.RegisterDateTimeTools(mcp)
	prometheus.RegisterPrometheusTools(mcp)
	helm.RegisterHelmTools(mcp)
	istio.RegisterIstioTools(mcp)
	argo.RegisterArgoTools(mcp)
	cilium.RegisterCiliumTools(mcp)
	grafana.RegisterGrafanaTools(mcp)
	docs.RegisterDocsTools(mcp)
}
