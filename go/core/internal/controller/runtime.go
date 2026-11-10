package controller

import (
	"context"
	"fmt"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	"k8s.io/client-go/rest"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/controller/apiclient"
	"github.com/kagent-dev/kagent/go/core/pkg/referencekind"
)

// Runtime owns the Kubernetes client and common options shared by the KRT
// collections. Collections must be created before Start so their informers are
// registered before the client starts.
type Runtime struct {
	Client      kube.Client
	Options     krt.OptionsBuilder
	Collections Collections
}

// NewRuntime creates the shared KRT client and collection graph. Handlers are
// deliberately registered separately at the eventual application boundary.
func NewRuntime(config *rest.Config, watchNamespaces []string, stop <-chan struct{}, settings *configv1.RuntimeDefaultsConfig, images *configv1.HarnessImagesConfig, referenceKinds func(kube.Client, []string, krt.OptionsBuilder) referencekind.Kinds) (*Runtime, error) {
	client, err := apiclient.New(config)
	if err != nil {
		return nil, fmt.Errorf("create KRT Kubernetes client: %w", err)
	}
	options := krt.NewOptionsBuilder(stop, "kagent", krt.GlobalDebugHandler)
	var kinds referencekind.Kinds
	if referenceKinds != nil {
		kinds = referenceKinds(client, watchNamespaces, options)
		if err := validateReferenceKinds(kinds); err != nil {
			return nil, err
		}
	}
	collections := NewCollections(client, watchNamespaces, options, settings, images, kinds)
	return &Runtime{Client: client, Options: options, Collections: collections}, nil
}

// validateReferenceKinds keeps registered kinds from replacing built-in ones.
func validateReferenceKinds(kinds referencekind.Kinds) error {
	if _, found := kinds.MCPServers[v1alpha3.RemoteMCPServerGroupKind]; found {
		return fmt.Errorf("reference kind %s is built in", v1alpha3.RemoteMCPServerGroupKind)
	}
	if _, found := kinds.ModelConfigs[v1alpha3.ModelConfigGroupKind]; found {
		return fmt.Errorf("reference kind %s is built in", v1alpha3.ModelConfigGroupKind)
	}
	return nil
}

// Start starts every informer registered by the KRT collections and keeps
// them alive until the application shuts down. The API uses installed CRDs
// directly, so it does not start Istio's cluster-wide delayed-CRD watcher.
func (r *Runtime) Start(ctx context.Context) error {
	defer r.Client.Shutdown()
	if !r.Client.RunAndWait(ctx.Done()) {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("sync KRT Kubernetes client")
	}
	<-ctx.Done()
	return nil
}
