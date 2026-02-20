package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/go-logr/logr"
)

// ServerConfig holds configuration for the A2A server.
type ServerConfig struct {
	Host            string
	Port            string
	ShutdownTimeout time.Duration
}

// A2AServer wraps the A2A server with health endpoints and graceful shutdown.
type A2AServer struct {
	httpServer *http.Server
	logger     logr.Logger
	config     ServerConfig
}

// NewA2AServer creates a new A2A server using a2asrv.
func NewA2AServer(agentCard a2atype.AgentCard, executor a2asrv.AgentExecutor, logger logr.Logger, config ServerConfig, handlerOpts ...a2asrv.RequestHandlerOption) (*A2AServer, error) {
	// Create request handler with the agent executor
	requestHandler := a2asrv.NewHandler(executor, handlerOpts...)

	// Create JSONRPC HTTP handler
	jsonrpcHandler := a2asrv.NewJSONRPCHandler(requestHandler)

	// Create mux to handle both A2A routes and health endpoints
	mux := http.NewServeMux()

	// Register health endpoints first
	RegisterHealthEndpoints(mux)

	// Register agent card endpoint
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(&agentCard))

	// All other routes go to the A2A JSONRPC handler
	mux.Handle("/", jsonrpcHandler)

	// Create HTTP server
	addr := ":" + config.Port
	if config.Host != "" {
		addr = config.Host + ":" + config.Port
	}

	return &A2AServer{
		httpServer: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
		logger: logger,
		config: config,
	}, nil
}

// Start initializes and starts the HTTP server.
func (s *A2AServer) Start() error {
	s.logger.Info("Starting Go ADK server!", "addr", s.httpServer.Addr)

	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error(err, "Server failed")
			os.Exit(1)
		}
	}()

	return nil
}

// WaitForShutdown blocks until a shutdown signal is received, then gracefully shuts down.
func (s *A2AServer) WaitForShutdown() error {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	<-stop
	s.logger.Info("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("error shutting down server: %w", err)
	}

	return nil
}

// Run starts the server and waits for shutdown.
func (s *A2AServer) Run() error {
	if err := s.Start(); err != nil {
		return err
	}
	return s.WaitForShutdown()
}
