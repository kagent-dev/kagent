package harness

import (
	"context"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubecrud"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Service struct {
	*kubecrud.Service[*v1alpha3.Harness, *v1alpha3.HarnessList]
}

func NewService(client client.Client, authorizer auth.Authorizer) *Service {
	return &Service{Service: kubecrud.NewService(
		client, authorizer, &v1alpha3.Harness{}, &v1alpha3.HarnessList{}, "Harness",
	)}
}

func (s *Service) Create(ctx context.Context, incoming *v1alpha3.Harness) (*v1alpha3.Harness, error) {
	if incoming == nil {
		return nil, serviceerrors.NewInvalidArgument("Harness resource is required", nil)
	}
	created := incoming.DeepCopy()
	created.Status = v1alpha3.HarnessStatus{}
	return s.Service.Create(ctx, created)
}
