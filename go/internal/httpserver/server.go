package httpserver

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/kagent-dev/kagent/go/internal/a2a"
	"github.com/kagent-dev/kagent/go/internal/database"
	"github.com/kagent-dev/kagent/go/internal/httpserver/handlers"
	"github.com/kagent-dev/kagent/go/internal/mcp"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/internal/version"
	"github.com/kagent-dev/kagent/go/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
	"k8s.io/apimachinery/pkg/types"
	ctrl_client "sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// API Path constants
	APIPathHealth          = "/health"
	APIPathVersion         = "/version"
	APIPathModelConfig     = "/api/modelconfigs"
	APIPathRuns            = "/api/runs"
	APIPathSessions        = "/api/sessions"
	APIPathTasks           = "/api/tasks"
	APIPathTools           = "/api/tools"
	APIPathToolServers     = "/api/toolservers"
	APIPathToolServerTypes = "/api/toolservertypes"
	APIPathAgents          = "/api/agents"
	APIPathProviders       = "/api/providers"
	APIPathModels          = "/api/models"
	APIPathMemories        = "/api/memories"
	APIPathNamespaces      = "/api/namespaces"
	APIPathA2A             = "/api/a2a"
	APIPathMCP             = "/mcp"
	APIPathFeedback        = "/api/feedback"
	APIPathLangGraph       = "/api/langgraph"
	APIPathCrewAI          = "/api/crewai"
)

var defaultModelConfig = types.NamespacedName{
	Name:      "default-model-config",
	Namespace: common.GetResourceNamespace(),
}

// ServerConfig holds the configuration for the HTTP server
type ServerConfig struct {
	Router            *mux.Router
	BindAddr          string
	KubeClient        ctrl_client.Client
	A2AHandler        a2a.A2AHandlerMux
	MCPHandler        *mcp.MCPHandler
	WatchedNamespaces []string
	DbClient          database.Client
	Authenticator     auth.AuthProvider
	Authorizer        auth.Authorizer
	ProxyURL          string
}

// HTTPServer is the structure that manages the HTTP server
type HTTPServer struct {
	httpServer    *http.Server
	config        ServerConfig
	router        *mux.Router
	handlers      *handlers.Handlers
	dbManager     *database.Manager
	authenticator auth.AuthProvider
}

// NewHTTPServer creates a new HTTP server instance
func NewHTTPServer(config ServerConfig) (*HTTPServer, error) {
	// Initialize database

	return &HTTPServer{
		config:        config,
		router:        config.Router,
		handlers:      handlers.NewHandlers(config.KubeClient, defaultModelConfig, config.DbClient, config.WatchedNamespaces, config.Authorizer, config.ProxyURL),
		authenticator: config.Authenticator,
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
	s.router.HandleFunc(APIPathSessions+"/{session_id}/tasks", adaptHandler(s.handlers.Sessions.HandleListTasksForSession)).Methods(http.MethodGet)
	s.router.HandleFunc(APIPathSessions+"/{session_id}", adaptHandler(s.handlers.Sessions.HandleDeleteSession)).Methods(http.MethodDelete)
	s.router.HandleFunc(APIPathSessions+"/{session_id}", adaptHandler(s.handlers.Sessions.HandleUpdateSession)).Methods(http.MethodPut)
	s.router.HandleFunc(APIPathSessions+"/{session_id}/events", adaptHandler(s.handlers.Sessions.HandleAddEventToSession)).Methods(http.MethodPost)

	// Tasks
	s.router.HandleFunc(APIPathTasks+"/{task_id}", adaptHandler(s.handlers.Tasks.HandleGetTask)).Methods(http.MethodGet)
	s.router.HandleFunc(APIPathTasks, adaptHandler(s.handlers.Tasks.HandleCreateTask)).Methods(http.MethodPost)
	s.router.HandleFunc(APIPathTasks+"/{task_id}", adaptHandler(s.handlers.Tasks.HandleDeleteTask)).Methods(http.MethodDelete)

	// Tools - using database handlers
	s.router.HandleFunc(APIPathTools, adaptHandler(s.handlers.Tools.HandleListTools)).Methods(http.MethodGet)

	// Tool Servers
	s.router.HandleFunc(APIPathToolServers, adaptHandler(s.handlers.ToolServers.HandleListToolServers)).Methods(http.MethodGet)
	s.router.HandleFunc(APIPathToolServers, adaptHandler(s.handlers.ToolServers.HandleCreateToolServer)).Methods(http.MethodPost)
	s.router.HandleFunc(APIPathToolServers+"/{namespace}/{name}", adaptHandler(s.handlers.ToolServers.HandleDeleteToolServer)).Methods(http.MethodDelete)

	// Tool Server Types
	s.router.HandleFunc(APIPathToolServerTypes, adaptHandler(s.handlers.ToolServerTypes.HandleListToolServerTypes)).Methods(http.MethodGet)

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

	// LangGraph Checkpoints
	s.router.HandleFunc(APIPathLangGraph+"/checkpoints", adaptHandler(s.handlers.Checkpoints.HandlePutCheckpoint)).Methods(http.MethodPost)
	s.router.HandleFunc(APIPathLangGraph+"/checkpoints", adaptHandler(s.handlers.Checkpoints.HandleListCheckpoints)).Methods(http.MethodGet)
	s.router.HandleFunc(APIPathLangGraph+"/checkpoints/writes", adaptHandler(s.handlers.Checkpoints.HandlePutWrites)).Methods(http.MethodPost)
	s.router.HandleFunc(APIPathLangGraph+"/checkpoints/{thread_id}", adaptHandler(s.handlers.Checkpoints.HandleDeleteThread)).Methods(http.MethodDelete)

	// CrewAI
	s.router.HandleFunc(APIPathCrewAI+"/memory", adaptHandler(s.handlers.CrewAI.HandleStoreMemory)).Methods(http.MethodPost)
	s.router.HandleFunc(APIPathCrewAI+"/memory", adaptHandler(s.handlers.CrewAI.HandleGetMemory)).Methods(http.MethodGet)
	s.router.HandleFunc(APIPathCrewAI+"/memory", adaptHandler(s.handlers.CrewAI.HandleResetMemory)).Methods(http.MethodDelete)
	s.router.HandleFunc(APIPathCrewAI+"/flows/state", adaptHandler(s.handlers.CrewAI.HandleStoreFlowState)).Methods(http.MethodPost)
	s.router.HandleFunc(APIPathCrewAI+"/flows/state", adaptHandler(s.handlers.CrewAI.HandleGetFlowState)).Methods(http.MethodGet)

	// A2A
	s.router.PathPrefix(APIPathA2A + "/{namespace}/{name}").Handler(s.config.A2AHandler)

	// MCP
	if s.config.MCPHandler != nil {
		s.router.PathPrefix(APIPathMCP).Handler(s.config.MCPHandler)
	}

	// Use middleware for common functionality
	s.router.Use(auth.AuthnMiddleware(s.authenticator))
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
