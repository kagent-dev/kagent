package httpserver

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/handlers"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	ctrl_client "sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// API Path constants
	APIPathHealth = "/health"
)

// ServerConfig holds the configuration for the HTTP server
type ServerConfig struct {
	Router        *mux.Router
	BindAddr      string
	KubeClient    ctrl_client.Client
	DbClient      dbpkg.Client
	Authenticator auth.AuthProvider

	// GrpcWebRouter composes this server's handler so that gRPC-Web calls reach the
	// gRPC services and everything else reaches the router. Supplied by the gRPC
	// server (see grpcserver.Server.WebHandlerOr), which owns the rule — there is
	// more than one binary serving HTTP beside that server, and a second copy of
	// the rule would drift.
	//
	// Optional: left nil, this server behaves exactly as it did before one existed.
	GrpcWebRouter func(next http.Handler) http.Handler
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
		config:        config,
		router:        config.Router,
		handlers:      handlers.NewHandlers(config.KubeClient),
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
		Handler: otelhttp.NewHandler(s.withGrpcWeb(s.router), "http.server",
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

// DbCleanupRunnable is a controller-runtime Runnable that periodically
// prunes expired DB rows (memory TTL, and when configured idle sessions).
// It implements NeedLeaderElection so that the sweep only runs on the
// elected leader, preventing duplicate deletes when multiple replicas are
// deployed.
type DbCleanupRunnable struct {
	DbClient             dbpkg.Client
	Interval             time.Duration
	SessionRetentionDays int
}

func (m *DbCleanupRunnable) NeedLeaderElection() bool { return true }

// NewDbCleanupRunnable returns a DbCleanupRunnable with the given database
// client. interval controls how often the cleanup runs; pass 0 to use the
// default of 24 hours. sessionRetentionDays is passed through to
// PruneExpiredSessions (0 disables session cleanup).
func NewDbCleanupRunnable(dbClient dbpkg.Client, interval time.Duration, sessionRetentionDays int) *DbCleanupRunnable {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	return &DbCleanupRunnable{
		DbClient:             dbClient,
		Interval:             interval,
		SessionRetentionDays: sessionRetentionDays,
	}
}

// Start runs the periodic cleanup loop until ctx is cancelled.
func (m *DbCleanupRunnable) Start(ctx context.Context) error {
	log := ctrllog.FromContext(ctx).WithName("db-cleanup")
	log.Info("Starting DB TTL cleanup loop", "interval", m.Interval, "sessionRetentionDays", m.SessionRetentionDays)
	ticker := time.NewTicker(m.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := m.DbClient.PruneExpiredMemories(ctx); err != nil {
				log.Error(err, "Failed to prune expired memories")
			}
			if deleted, err := m.DbClient.PruneExpiredSessions(ctx, m.SessionRetentionDays); err != nil {
				log.Error(err, "Failed to prune expired sessions")
			} else if deleted > 0 {
				log.Info("Pruned expired sessions", "deleted", deleted)
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

	// Use middleware for common functionality (first registered runs outermost on incoming requests).
	s.router.Use(wsAuthQueryMiddleware)
	s.router.Use(auth.AuthnMiddleware(s.authenticator))
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

func adaptHealthHandler(h func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return h
}

// withGrpcWeb lets the gRPC server route gRPC-Web calls ahead of this router.
//
// The split happens outside the router's middleware chain on purpose. That chain
// authenticates, rewrites content types and maps errors for the REST-shaped
// handlers, none of which a gRPC-Web frame survives — and it does not need any of
// it, because the gRPC server authenticates in its own interceptors. Routing these
// past the chain rather than through it keeps one protocol from being handled by
// the other's conventions.
func (s *HTTPServer) withGrpcWeb(next http.Handler) http.Handler {
	if s.config.GrpcWebRouter == nil {
		return next
	}
	return s.config.GrpcWebRouter(next)
}
