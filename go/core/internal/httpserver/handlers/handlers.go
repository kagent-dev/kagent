package handlers

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Handlers holds all the HTTP handler components
type Handlers struct {
	KubeClient client.Client
	Health     *HealthHandler
}

// NewHandlers creates a new Handlers instance with all handler components.
func NewHandlers(kubeClient client.Client) *Handlers {
	return &Handlers{
		KubeClient: kubeClient,
		Health:     NewHealthHandler(),
	}
}
