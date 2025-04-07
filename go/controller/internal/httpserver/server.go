package httpserver

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/handlers"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// Default namespace for resources
	DefaultResourceNamespace = "kagent"

	// API Path constants
	APIPathHealth      = "/health"
	APIPathModelConfig = "/api/modelconfigs"
	APIPathRuns        = "/api/runs"
	APIPathSessions    = "/api/sessions"
	APIPathTools       = "/api/tools"
	APIPathToolServers = "/api/toolservers"
	APIPathTeams       = "/api/teams"
)

var defaultModelConfig = types.NamespacedName{
	Name:      "default-model-config",
	Namespace: DefaultResourceNamespace,
}

// ServerConfig holds the configuration for the HTTP server
type ServerConfig struct {
	BindAddr      string
	AutogenClient *autogen_client.Client
	KubeClient    client.Client
}

// HTTPServer is the structure that manages the HTTP server
type HTTPServer struct {
	httpServer *http.Server
	config     ServerConfig
	router     *mux.Router
	handlers   *handlers.Handlers
}

// NewHTTPServer creates a new HTTP server instance
func NewHTTPServer(config ServerConfig) *HTTPServer {
	return &HTTPServer{
		config:   config,
		router:   mux.NewRouter(),
		handlers: handlers.NewHandlers(config.KubeClient, config.AutogenClient, defaultModelConfig),
	}
}

// Start initializes and starts the HTTP server
func (s *HTTPServer) Start(ctx context.Context) error {
	log := ctrllog.FromContext(ctx).WithName("http-server")
	log.Info("Starting HTTP server", "address", s.config.BindAddr)

	// Setup routes
	s.setupRoutes()

	// Create HTTP server
	s.httpServer = &http.Server{
		Addr:    s.config.BindAddr,
		Handler: s.router,
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

	// Model configs
	s.router.HandleFunc(APIPathModelConfig, adaptHandler(s.handlers.ModelConfig.HandleListModelConfigs)).Methods(http.MethodGet)
	s.router.HandleFunc(APIPathModelConfig+"/{configName}", adaptHandler(s.handlers.ModelConfig.HandleGetModelConfig)).Methods(http.MethodGet)

	// Runs
	s.router.HandleFunc(APIPathRuns, adaptHandler(s.handlers.Runs.HandleCreateRun)).Methods(http.MethodPost)
	s.router.HandleFunc(APIPathSessions+"/{sessionID}/runs", adaptHandler(s.handlers.Runs.HandleListSessionRuns)).Methods(http.MethodGet)

	// Sessions
	s.router.HandleFunc(APIPathSessions, adaptHandler(s.handlers.Sessions.HandleListSessions)).Methods(http.MethodGet)
	s.router.HandleFunc(APIPathSessions, adaptHandler(s.handlers.Sessions.HandleCreateSession)).Methods(http.MethodPost)
	s.router.HandleFunc(APIPathSessions+"/{sessionID}", adaptHandler(s.handlers.Sessions.HandleGetSession)).Methods(http.MethodGet)

	// Tools
	s.router.HandleFunc(APIPathTools, adaptHandler(s.handlers.Tools.HandleListTools)).Methods(http.MethodGet)

	// Tool Servers
	s.router.HandleFunc(APIPathToolServers, adaptHandler(s.handlers.ToolServers.HandleListToolServers)).Methods(http.MethodGet)
	s.router.HandleFunc(APIPathToolServers, adaptHandler(s.handlers.ToolServers.HandleCreateToolServer)).Methods(http.MethodPost)
	s.router.HandleFunc(APIPathToolServers+"/{toolServerName}", adaptHandler(s.handlers.ToolServers.HandleDeleteToolServer)).Methods(http.MethodDelete)

	// Teams
	s.router.HandleFunc(APIPathTeams, adaptHandler(s.handlers.Teams.HandleListTeams)).Methods(http.MethodGet)
	s.router.HandleFunc(APIPathTeams, adaptHandler(s.handlers.Teams.HandleCreateTeam)).Methods(http.MethodPost)
	s.router.HandleFunc(APIPathTeams, adaptHandler(s.handlers.Teams.HandleUpdateTeam)).Methods(http.MethodPut)
	s.router.HandleFunc(APIPathTeams+"/{teamID}", adaptHandler(s.handlers.Teams.HandleGetTeam)).Methods(http.MethodGet)
	s.router.HandleFunc(APIPathTeams+"/{teamLabel}", adaptHandler(s.handlers.Teams.HandleDeleteTeam)).Methods(http.MethodDelete)

	// Use middleware for common functionality
	s.router.Use(contentTypeMiddleware)
	s.router.Use(loggingMiddleware)
	s.router.Use(errorHandlerMiddleware)
}

func adaptHandler(h func(handlers.ErrorResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h(w.(handlers.ErrorResponseWriter), r)
	}
}

func adaptHealthHandler(h func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return h
}
