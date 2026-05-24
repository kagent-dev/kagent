package openshell

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openshell/channels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// UpsertMessagingProviders registers OpenShell gateway providers for harness channel credentials.
// Returns provider names to attach on CreateSandbox.spec.providers.
func UpsertMessagingProviders(
	ctx context.Context,
	oc *OpenShellClients,
	kube client.Client,
	ah *v1alpha2.AgentHarness,
) ([]string, error) {
	if ah == nil || len(ah.Spec.Channels) == 0 {
		return nil, nil
	}
	if oc == nil || oc.OpenShell == nil {
		return nil, fmt.Errorf("openshell: OpenShell client is required for messaging providers")
	}
	if kube == nil {
		return nil, fmt.Errorf("openshell: Kubernetes client is required for messaging providers")
	}

	resolved, err := channels.Resolve(ctx, kube, ah.Namespace, ah.Spec.Backend, ah.Spec.Channels)
	if err != nil {
		return nil, err
	}
	sandboxName := agentHarnessGatewayName(ah)
	msgDefs := channels.MessagingProviderDefs(sandboxName, resolved.Secrets, resolved)
	if len(msgDefs) == 0 {
		return nil, nil
	}
	gwDefs := messagingDefsToGateway(msgDefs)

	names := make([]string, 0, len(gwDefs))
	if err := UpsertGatewayProviders(ctx, oc.OpenShell, gwDefs); err != nil {
		return nil, err
	}
	for _, d := range gwDefs {
		names = append(names, d.Name)
	}
	ctrllog.FromContext(ctx).Info("upserted messaging providers",
		"agentHarness", ah.Namespace+"/"+ah.Name,
		"providers", names,
	)
	return names, nil
}
