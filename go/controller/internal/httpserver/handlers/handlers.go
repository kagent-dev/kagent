package handlers

import (
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
)

// Handlers holds all the HTTP handler components
type Handlers struct {
	Health      *HealthHandler
	ModelConfig *ModelConfigHandler
	Model       *ModelHandler
	Provider    *ProviderHandler
	Sessions    *SessionsHandler
	Teams       *TeamsHandler
	Tools       *ToolsHandler
	ToolServers *ToolServersHandler
	Invoke      *InvokeHandler
	Memory      *MemoryHandler
	Feedback    *FeedbackHandler
	Namespaces  *NamespacesHandler
}

// Base holds common dependencies for all handlers
type Base struct {
	KubeClient         client.Client
	AutogenClient      autogen_client.Client
	DefaultModelConfig types.NamespacedName
}

// NewHandlers creates a new Handlers instance with all handler components
func NewHandlers(kubeClient client.Client, autogenClient autogen_client.Client, defaultModelConfig types.NamespacedName, watchedNamespaces []string) *Handlers {
	base := &Base{
		KubeClient:         kubeClient,
		AutogenClient:      autogenClient,
		DefaultModelConfig: defaultModelConfig,
	}

	return &Handlers{
		Health:      NewHealthHandler(),
		ModelConfig: NewModelConfigHandler(base),
		Model:       NewModelHandler(base),
		Provider:    NewProviderHandler(base),
		Sessions:    NewSessionsHandler(base),
		Teams:       NewTeamsHandler(base),
		Tools:       NewToolsHandler(base),
		ToolServers: NewToolServersHandler(base),
		Invoke:      NewInvokeHandler(base),
		Memory:      NewMemoryHandler(base),
		Feedback:    NewFeedbackHandler(base),
		Namespaces:  NewNamespacesHandler(base, watchedNamespaces),
	}
}
