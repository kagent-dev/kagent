package apiclient

import (
	ateclient "github.com/agent-substrate/substrate/pkg/client/clientset/versioned"
	kagentclient "github.com/kagent-dev/kagent/go/api/clientset/versioned"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube"
	"k8s.io/client-go/rest"
)

// Client is the Kubernetes client KRT receives. Embedding the generated
// clientsets lets type registration use their normal list and watch paths.
type Client interface {
	kube.Client
	Kagent() kagentclient.Interface
	Substrate() ateclient.Interface
}

type client struct {
	kube.Client
	kagent    kagentclient.Interface
	substrate ateclient.Interface
}

func New(config *rest.Config) (Client, error) {
	kubeClient, err := kube.NewClient(kube.NewClientConfigForRestConfig(config), cluster.ID("kagent"))
	if err != nil {
		return nil, err
	}
	kagent, err := kagentclient.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	substrate, err := ateclient.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	RegisterTypes()
	return &client{Client: kubeClient, kagent: kagent, substrate: substrate}, nil
}

func (c *client) Kagent() kagentclient.Interface {
	return c.kagent
}

func (c *client) Substrate() ateclient.Interface {
	return c.substrate
}
