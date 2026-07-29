package httpserver

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/core/internal/a2a"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/handlers"
	"github.com/kagent-dev/kagent/go/core/internal/mcp"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	ctrl_client "sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// API Path constants
	APIPathHealth               = "/health"
	APIPathA2A                  = "/api/a2a"
	APIPathA2ASandboxes         = "/api/a2a-sandboxes"
	APIPathMCP                  = "/mcp"
	APIPathAgentHarnessACP      = "/api/agentharnesses/{namespace}/{name}/acp/"
)

// ServerConfig holds the configuration for the HTTP server
type ServerConfig struct {
	Router                       *mux.Router
	BindAddr                     string
	KubeClient                   ctrl_client.Client
	A2AHandler                   a2a.A2AHandlerMux
	MCPHandler                   *mcp.MCPHandler
	DbClient                     dbpkg.Client
	Authenticator                auth.AuthProvider
	AgentHarnessGateway          *handlers.AgentHarnessGatewayConfig
	AgentHarnessSessionActor     *substrate.AgentHarnessSessionActorBackend
}

// HTTPServer is the structure that manages the HTTP server
type HTTPServer struct {
	httpServer    *http.Server
	config        ServerConfig
	router        *mux.Router
	handlers      *handlers.Handlers
	authenticator auth.AuthProvider
}

// NewHTTPServer creates a new HTTP server instance
func NewHTTPServer(config ServerConfig) (*HTTPServer, error) {
	// Initialize database

	return &HTTPServer{
		config: config,
		router: config.Router,
		handlers: handlers.NewHandlers(
			config.KubeClient,
			config.AgentHarnessGateway,
			config.AgentHarnessSessionActor,
		),
		authenticator: config.Authenticator,
	}, nil
}

// Start initializes and starts the HTTP server
func (s *HTTPServer) Start(ctx context.Context) error {
	log := ctrllog.FromContext(ctx).WithName("http-server")
	log.Info("Starting HTTP server", "address", s.config.BindAddr)

	// Setup routes
	s.setupRoutes()

	// Create HTTP server, wrapping the router with otelhttp for span creation
	// and W3C TraceContext propagation on every incoming request.
	s.httpServer = &http.Server{
		Addr: s.config.BindAddr,
		Handler: otelhttp.NewHandler(s.router, "http.server",
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				return r.Method + " " + r.URL.Path
			}),
			otelhttp.WithFilter(func(r *http.Request) bool {
				return r.URL.Path != APIPathHealth
			}),
		),
	}

	// Start the server in a separate goroutine
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error(err, "HTTP server failed")
		}
	}()

	// Wait for context cancellation to shut down
	go func() {
		<-ctx.Done()
		log.Info("Shutting down HTTP server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			log.Error(err, "Failed to properly shutdown HTTP server")
		}
	}()

	return nil
}

// MemoryCleanupRunnable is a controller-runtime Runnable that periodically
// prunes expired memory entries. It implements NeedLeaderElection so that
// the sweep only runs on the elected leader, preventing duplicate deletes
// when multiple replicas are deployed.
type MemoryCleanupRunnable struct {
	DbClient dbpkg.Client
	Interval time.Duration
}

func (m *MemoryCleanupRunnable) NeedLeaderElection() bool { return true }

// NewMemoryCleanupRunnable returns a MemoryCleanupRunnable with the given
// database client. interval controls how often the cleanup runs; pass 0 to
// use the default of 24 hours.
func NewMemoryCleanupRunnable(dbClient dbpkg.Client, interval time.Duration) *MemoryCleanupRunnable {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	return &MemoryCleanupRunnable{DbClient: dbClient, Interval: interval}
}

// Start runs the periodic cleanup loop until ctx is cancelled.
func (m *MemoryCleanupRunnable) Start(ctx context.Context) error {
	log := ctrllog.FromContext(ctx).WithName("memory-cleanup")
	log.Info("Starting memory TTL cleanup loop", "interval", m.Interval)
	ticker := time.NewTicker(m.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := m.DbClient.PruneExpiredMemories(ctx); err != nil {
				log.Error(err, "Failed to prune expired memories")
			}
		case <-ctx.Done():
			return nil
		}
	}
}

// Stop stops the HTTP server
func (s *HTTPServer) Stop(ctx context.Context) error {
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// NeedLeaderElection implements controller-runtime's LeaderElectionRunnable interface
func (s *HTTPServer) NeedLeaderElection() bool {
	// Return false so the HTTP server runs on all instances, not just the leader
	return false
}

// setupRoutes configures all the routes for the server
func (s *HTTPServer) setupRoutes() {
	// Health check endpoint
	s.router.HandleFunc(APIPathHealth, adaptHealthHandler(s.handlers.Health.HandleHealth)).Methods(http.MethodGet)

	// Substrate harness /acp WebSocket proxy via atenet-router.
	s.router.PathPrefix(APIPathAgentHarnessACP).Handler(
		adaptHandler(s.handlers.HandleAgentHarnessGateway),
	)

	// A2A
	s.router.PathPrefix(APIPathA2A + "/{namespace}/{name}").Handler(s.config.A2AHandler)
	s.router.PathPrefix(APIPathA2ASandboxes + "/{namespace}/{name}").Handler(s.config.A2AHandler)

	// MCP
	if s.config.MCPHandler != nil {
		s.router.PathPrefix(APIPathMCP).Handler(s.config.MCPHandler)
	}

	// Use middleware for common functionality (first registered runs outermost on incoming requests).
	s.router.Use(wsAuthQueryMiddleware)
	s.router.Use(auth.AuthnMiddleware(s.authenticator))
	s.router.Use(s.shareTokenMiddleware)
	s.router.Use(contentTypeMiddleware)
	s.router.Use(loggingMiddleware)
	s.router.Use(errorHandlerMiddleware)
}

// wsAuthQueryMiddleware maps token query params → Authorization for browser WebSocket upgrades
// (fetch can send headers; WebSocket cannot).
func wsAuthQueryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			next.ServeHTTP(w, r)
			return
		}
		var token string
		switch {
		case strings.HasSuffix(r.URL.Path, "/ssh"):
			token = r.URL.Query().Get("access_token")
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
		}
		next.ServeHTTP(w, r)
	})
}

func adaptHandler(h func(handlers.ErrorResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h(w.(handlers.ErrorResponseWriter), r)
	}
}

func adaptHealthHandler(h func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return h
}
