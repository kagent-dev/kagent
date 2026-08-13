package controller

import (
	"context"
	"fmt"

	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	"k8s.io/client-go/rest"
)

// Runtime owns the Kubernetes client and common options shared by the v2 KRT
// collections. Collections must be created before Start so their informers are
// registered before the client starts.
type Runtime struct {
	Client  kube.Client
	Options krt.OptionsBuilder
}

// NewRuntime creates the shared KRT client without registering any controller
// collections or handlers.
func NewRuntime(config *rest.Config, stop <-chan struct{}) (*Runtime, error) {
	client, err := kube.NewClient(kube.NewClientConfigForRestConfig(config), cluster.ID("kagent"))
	if err != nil {
		return nil, fmt.Errorf("create KRT Kubernetes client: %w", err)
	}
	return &Runtime{
		Client:  client,
		Options: krt.NewOptionsBuilder(stop, "kagent", krt.GlobalDebugHandler),
	}, nil
}

// Start starts every informer registered by the v2 KRT collections and keeps
// them alive until the application shuts down. The v2 API uses installed CRDs
// directly, so it does not start Istio's cluster-wide delayed-CRD watcher.
func (r *Runtime) Start(ctx context.Context) error {
	factory := r.Client.Informers()
	factory.Start(ctx.Done())
	if !factory.WaitForCacheSync(ctx.Done()) {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("sync KRT Kubernetes client")
	}
	<-ctx.Done()
	r.Client.Shutdown()
	return nil
}
