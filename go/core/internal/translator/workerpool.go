package translator

import (
	"fmt"
	"slices"

	atev1 "go.ate.dev/substrate/api/infra/k8s/ate/v1"
	"istio.io/istio/pkg/kube/krt"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// ResolveWorkerPool returns the WorkerPool that a Substrate policy in source
// references. A pool in another namespace is allowed only when the
// installation lists that namespace in substrate.workerPoolNamespaces.
func ResolveWorkerPool(ctx krt.HandlerContext, collections Collections, source string, ref v1alpha3.WorkerPoolReference) (*atev1.WorkerPool, error) {
	key := ref.NamespacedName(source)
	if key.Namespace != source && !slices.Contains(collections.WorkerPoolNamespaces, key.Namespace) {
		return nil, fmt.Errorf("reference from namespace %q to WorkerPool %q denied: namespace %q is not in substrate.workerPoolNamespaces", source, key, key.Namespace)
	}
	pool := krt.FetchOne(ctx, collections.WorkerPools, krt.FilterObjectName(key))
	if pool == nil {
		return nil, &WorkerPoolNotFoundError{WorkerPool: key}
	}
	return *pool, nil
}
