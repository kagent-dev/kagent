package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"trpc.group/trpc-go/trpc-a2a-go/server"
	"trpc.group/trpc-go/trpc-a2a-go/taskmanager"
)

// ServerConfig holds configuration for the A2A server.
type ServerConfig struct {
	// Host is the address to bind to (empty binds to all interfaces)
	Host string

	// Port is the port to listen on
	Port string

	// ShutdownTimeout is the timeout for graceful shutdown
	ShutdownTimeout time.Duration
}

// DefaultServerConfig returns the default server configuration.
func DefaultServerConfig() ServerConfig {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	return ServerConfig{
		Host:            "",
		Port:            port,
		ShutdownTimeout: 5 * time.Second,
	}
}

// A2AServer wraps the A2A server with health endpoints and graceful shutdown.
type A2AServer struct {
	agentCard   server.AgentCard
	taskManager taskmanager.TaskManager
	httpServer  *http.Server
	logger      logr.Logger
	config      ServerConfig
}

// NewA2AServer creates a new A2A server wrapper.
func NewA2AServer(agentCard server.AgentCard, taskManager taskmanager.TaskManager, logger logr.Logger, config ServerConfig) (*A2AServer, error) {
	return &A2AServer{
		agentCard:   agentCard,
		taskManager: taskManager,
		logger:      logger,
		config:      config,
	}, nil
}

// Start initializes and starts the HTTP server.
func (s *A2AServer) Start() error {
	// Initialize A2A server with agent card
	a2aServer, err := server.NewA2AServer(s.agentCard, s.taskManager)
	if err != nil {
		return fmt.Errorf("failed to create A2A server: %w", err)
	}

	// Create mux to handle both A2A routes and health endpoints
	mux := http.NewServeMux()

	// Register health endpoints first (before catch-all "/" route)
	RegisterHealthEndpoints(mux)

	// All other routes go to A2A server
	mux.Handle("/", a2aServer.Handler())

	// Create HTTP server
	addr := ":" + s.config.Port
	if s.config.Host != "" {
		addr = s.config.Host + ":" + s.config.Port
	}
	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	s.logger.Info("Starting Go ADK server!", "addr", addr, "host", s.config.Host, "port", s.config.Port)

	// Start server in goroutine
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
