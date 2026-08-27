package translator

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

// Reader is the only Kubernetes capability used while compiling a revision.
// KRT supplies a dependency-tracking implementation; tests may use any reader.
type Reader interface {
	Get(context.Context, types.NamespacedName, runtime.Object) error
	GetResolvedModelConfig(context.Context, types.NamespacedName) (*ResolvedModelConfig, error)
}
