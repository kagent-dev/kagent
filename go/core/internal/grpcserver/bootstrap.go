package grpcserver

import (
	"context"

	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
	"github.com/kagent-dev/kagent/go/core/internal/service/bootstrap"
)

type bootstrapServer struct {
	runtimev1.UnimplementedBootstrapServiceServer
	service *bootstrap.Service
}

var _ runtimev1.BootstrapServiceServer = (*bootstrapServer)(nil)

func (s *bootstrapServer) GetBootstrapConfig(ctx context.Context, _ *runtimev1.BootstrapServiceGetBootstrapConfigRequest) (*runtimev1.BootstrapServiceGetBootstrapConfigResponse, error) {
	return s.service.Get(ctx)
}
