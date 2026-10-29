package grpcserver

import (
	"context"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/service/agentidentity"
)

type agentIdentityServer struct {
	apiv1.UnimplementedAgentIdentityServiceServer
	service *agentidentity.Service
}

var _ apiv1.AgentIdentityServiceServer = (*agentIdentityServer)(nil)

func (s *agentIdentityServer) ResolveAgentIdentity(ctx context.Context, _ *apiv1.ResolveAgentIdentityRequest) (*apiv1.ResolveAgentIdentityResponse, error) {
	return s.service.Resolve(ctx)
}
