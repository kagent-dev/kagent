// Package referencekind lets an embedding controller add kinds that model and
// MCP server references may name.
package referencekind

import (
	"istio.io/istio/pkg/kube/krt"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// Kinds maps a reference's group and kind to its objects, projected to the built-in type.
// A projection sets TypeMeta to the source kind and copies its ObjectMeta and status.
type Kinds struct {
	MCPServers   map[schema.GroupKind]krt.Collection[*v1alpha3.RemoteMCPServer]
	ModelConfigs map[schema.GroupKind]krt.Collection[*v1alpha3.ModelConfig]
}
