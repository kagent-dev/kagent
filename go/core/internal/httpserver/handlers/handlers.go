package handlers

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/core/internal/controller/reconciler"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"github.com/kagent-dev/kagent/go/core/pkg/translator"
)

// SessionHook is called by session HTTP handlers on session lifecycle events.
// Implementations must be safe to call concurrently.
type SessionHook interface {
	// OnSessionCreated is called after the session row is written to the DB.
	OnSessionCreated(ctx context.Context, session *database.Session, agent *database.Agent) error
	// OnSessionDeleted is called before the session row is removed from the DB.
	OnSessionDeleted(ctx context.Context, sessionID, userID string) error
}

// Handlers holds all the HTTP handler components
type Handlers struct {
	Health              *HealthHandler
	ModelConfig         *ModelConfigHandler
	Model               *ModelHandler
	ModelProviderConfig *ModelProviderConfigHandler
	Sessions            *SessionsHandler
	Agents              *AgentsHandler
	Tools               *ToolsHandler
	ToolServers         *ToolServersHandler
	ToolServerTypes     *ToolServerTypesHandler
	Memory              *MemoryHandler
	Feedback            *FeedbackHandler
	Namespaces          *NamespacesHandler
	PromptTemplates     *PromptTemplatesHandler
	Tasks               *TasksHandler
	Checkpoints         *CheckpointsHandler
	CrewAI              *CrewAIHandler
	CurrentUser         *CurrentUserHandler
}

// Base holds common dependencies for all handlers
type Base struct {
	KubeClient           client.Client
	DefaultModelConfig   types.NamespacedName
	DatabaseService      database.Client
	Authorizer           auth.Authorizer // Interface for authorization checks
	ProxyURL             string
	WatchedNamespaces    []string
	SandboxBackend       sandboxbackend.Backend
	WorkloadModeResolver translator.WorkloadModeResolver
	// SessionHook is called on session lifecycle events. Optional.
	SessionHook SessionHook
}

// NewHandlers creates a new Handlers instance with all handler components.
func NewHandlers(kubeClient client.Client, defaultModelConfig types.NamespacedName, dbService database.Client, watchedNamespaces []string, authorizer auth.Authorizer, proxyURL string, rcnclr reconciler.KagentReconciler, sandboxBackend sandboxbackend.Backend, workloadModeResolver translator.WorkloadModeResolver, sessionHook SessionHook) *Handlers {
	base := &Base{
		KubeClient:           kubeClient,
		DefaultModelConfig:   defaultModelConfig,
		DatabaseService:      dbService,
		Authorizer:           authorizer,
		ProxyURL:             proxyURL,
		WatchedNamespaces:    watchedNamespaces,
		SandboxBackend:       sandboxBackend,
		WorkloadModeResolver: workloadModeResolver,
		SessionHook:          sessionHook,
	}

	return &Handlers{
		Health:              NewHealthHandler(),
		ModelConfig:         NewModelConfigHandler(base),
		Model:               NewModelHandler(base),
		ModelProviderConfig: NewModelProviderConfigHandler(base, rcnclr),
		Sessions:            NewSessionsHandler(base),
		Agents:              NewAgentsHandler(base),
		Tools:               NewToolsHandler(base),
		ToolServers:         NewToolServersHandler(base),
		ToolServerTypes:     NewToolServerTypesHandler(base),
		Memory:              NewMemoryHandler(base),
		Feedback:            NewFeedbackHandler(base),
		Namespaces:          NewNamespacesHandler(base),
		PromptTemplates:     NewPromptTemplatesHandler(base),
		Tasks:               NewTasksHandler(base),
		Checkpoints:         NewCheckpointsHandler(base),
		CrewAI:              NewCrewAIHandler(base),
		CurrentUser:         NewCurrentUserHandler(),
	}
}
