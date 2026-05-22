package translator

import (
	"context"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type AgentOutputs struct {
	Manifest []client.Object `json:"manifest,omitempty"`

	Config    *adk.AgentConfig `json:"config,omitempty"`
	AgentCard a2a.AgentCard    `json:"agentCard"`
}

type TranslatorPlugin interface {
	ProcessAgent(ctx context.Context, agent v1alpha2.AgentObject, outputs *AgentOutputs) error
	GetOwnedResourceTypes() []client.Object
}
