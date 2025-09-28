package translator

import (
	"context"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/adk"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"trpc.group/trpc-go/trpc-a2a-go/server"
)

type AgentOutputs struct {
	Manifest []client.Object `json:"manifest,omitempty"`

	Config    *adk.AgentConfig `json:"config,omitempty"`
	AgentCard server.AgentCard `json:"agentCard"`
}

type TranslatorPlugin func(ctx context.Context, agent *v1alpha2.Agent, outputs *AgentOutputs) error
