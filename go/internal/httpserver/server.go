package httpserver

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/kagent-dev/kagent/go/internal/a2a"
	autogen_client "github.com/kagent-dev/kagent/go/internal/autogen/client"
	"github.com/kagent-dev/kagent/go/internal/database"
	"github.com/kagent-dev/kagent/go/internal/httpserver/handlers"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/internal/version"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
	"k8s.io/apimachinery/pkg/types"
	ctrl_client "sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// API Path constants
	APIPathHealth      = "/health"
	APIPathVersion     = "/version"
	APIPathModelConfig = "/api/modelconfigs"
	APIPathRuns        = "/api/runs"
	APIPathSessions    = "/api/sessions"
	APIPathTools       = "/api/tools"
	APIPathToolServers = "/api/toolservers"
	APIPathAgents      = "/api/agents"
	APIPathProviders   = "/api/providers"
	APIPathModels      = "/api/models"
	APIPathMemories    = "/api/memories"
	APIPathNamespaces  = "/api/namespaces"
	APIPathA2A         = "/api/a2a"
	APIPathFeedback    = "/api/feedback"
)

var defaultModelConfig = types.NamespacedName{
	Name:      "default-model-config",
	Namespace: common.GetResourceNamespace(),
}

// ServerConfig holds the configuration for the HTTP server
type ServerConfig struct {
	BindAddr          string
	AutogenClient     autogen_client.Client
	KubeClient        ctrl_client.Client
	A2AHandler        a2a.A2AHandlerMux
	WatchedNamespaces []string
	DbClient          database.Client
}

// HTTPServer is the structure that manages the HTTP server
type HTTPServer struct {
	httpServer *http.Server
	config     ServerConfig
	router     *mux.Router
	handlers   *handlers.Handlers
	dbManager  *database.Manager
	dbClient   database.Client
}

// NewHTTPServer creates a new HTTP server instance
func NewHTTPServer(config ServerConfig) (*HTTPServer, error) {
	// Initialize database

	return &HTTPServer{
		config:   config,
		router:   mux.NewRouter(),
		handlers: handlers.NewHandlers(config.KubeClient, config.AutogenClient, defaultModelConfig, config.DbClient, config.WatchedNamespaces),
	}, nil
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
		// Close database connection
		if err := s.dbManager.Close(); err != nil {
			log.Error(err, "Failed to close database connection")
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

	// Version
	s.router.HandleFunc(APIPathVersion, adaptHandler(func(erw handlers.ErrorResponseWriter, r *http.Request) {
		versionResponse := api.VersionResponse{
			KAgentVersion: version.Version,
			GitCommit:     version.GitCommit,
			BuildDate:     version.BuildDate,
		}
		handlers.RespondWithJSON(erw, http.StatusOK, versionResponse)
	})).Methods(http.MethodGet)

	// Model configs
	s.router.HandleFunc(APIPathModelConfig, adaptHandler(s.handlers.ModelConfig.HandleListModelConfigs)).Methods(http.MethodGet)
	s.router.HandleFunc(APIPathModelConfig+"/{namespace}/{name}", adaptHandler(s.handlers.ModelConfig.HandleGetModelConfig)).Methods(http.MethodGet)
	s.router.HandleFunc(APIPathModelConfig, adaptHandler(s.handlers.ModelConfig.HandleCreateModelConfig)).Methods(http.MethodPost)
	s.router.HandleFunc(APIPathModelConfig+"/{namespace}/{name}", adaptHandler(s.handlers.ModelConfig.HandleDeleteModelConfig)).Methods(http.MethodDelete)
	s.router.HandleFunc(APIPathModelConfig+"/{namespace}/{name}", adaptHandler(s.handlers.ModelConfig.HandleUpdateModelConfig)).Methods(http.MethodPut)

	// Sessions - using database handlers
	s.router.HandleFunc(APIPathSessions, adaptHandler(s.handlers.Sessions.HandleListSessions)).Methods(http.MethodGet)
	s.router.HandleFunc(APIPathSessions, adaptHandler(s.handlers.Sessions.HandleCreateSession)).Methods(http.MethodPost)
	s.router.HandleFunc(APIPathSessions+"/agent/{namespace}/{name}", adaptHandler(s.handlers.Sessions.HandleGetSessionsForAgent)).Methods(http.MethodGet)
	s.router.HandleFunc(APIPathSessions+"/{session_id}", adaptHandler(s.handlers.Sessions.HandleGetSession)).Methods(http.MethodGet)
	s.router.HandleFunc(APIPathSessions+"/{session_id}/messages", adaptHandler(s.handlers.Sessions.HandleListSessionMessages)).Methods(http.MethodGet)
	s.router.HandleFunc(APIPathSessions+"/{session_id}/tasks", adaptHandler(s.handlers.Sessions.HandleListSessionTasks)).Methods(http.MethodGet)
	s.router.HandleFunc(APIPathSessions+"/{session_id}", adaptHandler(s.handlers.Sessions.HandleDeleteSession)).Methods(http.MethodDelete)
	s.router.HandleFunc(APIPathSessions+"/{session_id}", adaptHandler(s.handlers.Sessions.HandleUpdateSession)).Methods(http.MethodPut)
	s.router.HandleFunc(APIPathSessions+"/{session_id}/invoke/stream", adaptHandler(s.handlers.Sessions.HandleInvokeSessionStream)).Methods(http.MethodPost)
	s.router.HandleFunc(APIPathSessions+"/{session_id}/invoke", adaptHandler(s.handlers.Sessions.HandleInvokeSession)).Methods(http.MethodPost)

	// Tools - using database handlers
	s.router.HandleFunc(APIPathTools, adaptHandler(s.handlers.Tools.HandleListTools)).Methods(http.MethodGet)

	// Tool Servers
	s.router.HandleFunc(APIPathToolServers, adaptHandler(s.handlers.ToolServers.HandleListToolServers)).Methods(http.MethodGet)
	s.router.HandleFunc(APIPathToolServers, adaptHandler(s.handlers.ToolServers.HandleCreateToolServer)).Methods(http.MethodPost)
	s.router.HandleFunc(APIPathToolServers+"/{namespace}/{name}", adaptHandler(s.handlers.ToolServers.HandleDeleteToolServer)).Methods(http.MethodDelete)

	// Agents - using database handlers
	s.router.HandleFunc(APIPathAgents, adaptHandler(s.handlers.Agents.HandleListAgents)).Methods(http.MethodGet)
	s.router.HandleFunc(APIPathAgents, adaptHandler(s.handlers.Agents.HandleCreateAgent)).Methods(http.MethodPost)
	s.router.HandleFunc(APIPathAgents, adaptHandler(s.handlers.Agents.HandleUpdateAgent)).Methods(http.MethodPut)
	s.router.HandleFunc(APIPathAgents+"/{namespace}/{name}", adaptHandler(s.handlers.Agents.HandleGetAgent)).Methods(http.MethodGet)
	s.router.HandleFunc(APIPathAgents+"/{namespace}/{name}", adaptHandler(s.handlers.Agents.HandleDeleteAgent)).Methods(http.MethodDelete)

	// Providers
	s.router.HandleFunc(APIPathProviders+"/models", adaptHandler(s.handlers.Provider.HandleListSupportedModelProviders)).Methods(http.MethodGet)
	s.router.HandleFunc(APIPathProviders+"/memories", adaptHandler(s.handlers.Provider.HandleListSupportedMemoryProviders)).Methods(http.MethodGet)

	// Models
	s.router.HandleFunc(APIPathModels, adaptHandler(s.handlers.Model.HandleListSupportedModels)).Methods(http.MethodGet)

	// Memories
	s.router.HandleFunc(APIPathMemories, adaptHandler(s.handlers.Memory.HandleListMemories)).Methods(http.MethodGet)
	s.router.HandleFunc(APIPathMemories, adaptHandler(s.handlers.Memory.HandleCreateMemory)).Methods(http.MethodPost)
	s.router.HandleFunc(APIPathMemories+"/{namespace}/{name}", adaptHandler(s.handlers.Memory.HandleDeleteMemory)).Methods(http.MethodDelete)
	s.router.HandleFunc(APIPathMemories+"/{namespace}/{name}", adaptHandler(s.handlers.Memory.HandleGetMemory)).Methods(http.MethodGet)
	s.router.HandleFunc(APIPathMemories+"/{namespace}/{name}", adaptHandler(s.handlers.Memory.HandleUpdateMemory)).Methods(http.MethodPut)

	// Namespaces
	s.router.HandleFunc(APIPathNamespaces, adaptHandler(s.handlers.Namespaces.HandleListNamespaces)).Methods(http.MethodGet)

	// Feedback - using database handlers
	s.router.HandleFunc(APIPathFeedback, adaptHandler(s.handlers.Feedback.HandleCreateFeedback)).Methods(http.MethodPost)
	s.router.HandleFunc(APIPathFeedback, adaptHandler(s.handlers.Feedback.HandleListFeedback)).Methods(http.MethodGet)

	// A2A
	s.router.PathPrefix(APIPathA2A).Handler(s.config.A2AHandler)

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
