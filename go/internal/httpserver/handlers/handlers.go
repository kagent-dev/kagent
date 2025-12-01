package handlers

import (
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/internal/database"
	"github.com/kagent-dev/kagent/go/pkg/auth"
)

// Handlers holds all the HTTP handler components
type Handlers struct {
	Health          *HealthHandler
	ModelConfig     *ModelConfigHandler
	Model           *ModelHandler
	Provider        *ProviderHandler
	Sessions        *SessionsHandler
	Agents          *AgentsHandler
	Tools           *ToolsHandler
	ToolServers     *ToolServersHandler
	ToolServerTypes *ToolServerTypesHandler
	Memory          *MemoryHandler
	Feedback        *FeedbackHandler
	Namespaces      *NamespacesHandler
	Tasks           *TasksHandler
	Checkpoints     *CheckpointsHandler
	CrewAI          *CrewAIHandler
}

// Base holds common dependencies for all handlers
type Base struct {
	KubeClient         client.Client
	DefaultModelConfig types.NamespacedName
	DatabaseService    database.Client
	Authorizer         auth.Authorizer // Interface for authorization checks
}

// NewHandlers creates a new Handlers instance with all handler components
func NewHandlers(kubeClient client.Client, defaultModelConfig types.NamespacedName, dbService database.Client, watchedNamespaces []string, authorizer auth.Authorizer) *Handlers {
	base := &Base{
		KubeClient:         kubeClient,
		DefaultModelConfig: defaultModelConfig,
		DatabaseService:    dbService,
		Authorizer:         authorizer,
	}

	return &Handlers{
		Health:          NewHealthHandler(),
		ModelConfig:     NewModelConfigHandler(base),
		Model:           NewModelHandler(base),
		Provider:        NewProviderHandler(base),
		Sessions:        NewSessionsHandler(base),
		Agents:          NewAgentsHandler(base),
		Tools:           NewToolsHandler(base),
		ToolServers:     NewToolServersHandler(base),
		ToolServerTypes: NewToolServerTypesHandler(base),
		Memory:          NewMemoryHandler(base),
		Feedback:        NewFeedbackHandler(base),
		Namespaces:      NewNamespacesHandler(base, watchedNamespaces),
		Tasks:           NewTasksHandler(base),
		Checkpoints:     NewCheckpointsHandler(base),
		CrewAI:          NewCrewAIHandler(base),
	}
}
