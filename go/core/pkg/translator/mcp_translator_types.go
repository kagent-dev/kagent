package translator

import (
	"context"

	"github.com/kagent-dev/kmcp/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type MCPTranslatorPlugin func(
	ctx context.Context,
	server *v1alpha1.MCPServer,
	objects []client.Object,
) ([]client.Object, error)
