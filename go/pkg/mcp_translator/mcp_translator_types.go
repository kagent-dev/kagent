package mcp_translator

import (
	"context"

	"github.com/kagent-dev/kmcp/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type TranslatorPlugin func(
	ctx context.Context,
	server *v1alpha1.MCPServer,
	objects []client.Object,
) ([]client.Object, error)
