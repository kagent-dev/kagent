package handlers

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
)

// Handlers holds all the HTTP handler components
type Handlers struct {
	KubeClient          client.Client
	AgentHarnessGateway *AgentHarnessGatewayConfig
	// AgentHarnessSessionActor creates/suspends the per-session substrate actors
	// that back each AgentHarness chat session.
	AgentHarnessSessionActor *substrate.AgentHarnessSessionActorBackend

	Health *HealthHandler
}

// NewHandlers creates a new Handlers instance with all handler components.
func NewHandlers(
	kubeClient client.Client,
	agentHarnessGateway *AgentHarnessGatewayConfig,
	agentHarnessSessionActorBackend *substrate.AgentHarnessSessionActorBackend,
) *Handlers {
	return &Handlers{
		KubeClient:               kubeClient,
		AgentHarnessGateway:      agentHarnessGateway,
		AgentHarnessSessionActor: agentHarnessSessionActorBackend,
		Health:                   NewHealthHandler(),
	}
}
