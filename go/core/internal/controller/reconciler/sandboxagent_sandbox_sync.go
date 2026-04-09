package reconciler

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	agentsandboxv1 "sigs.k8s.io/agent-sandbox/api/v1alpha1"
	extensionsv1alpha1 "sigs.k8s.io/agent-sandbox/extensions/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// sandboxTemplateResourceVersionAnnotation records the SandboxTemplate.metadata.resourceVersion
// last applied to the live Sandbox workload. agent-sandbox's SandboxClaim controller only copies
// PodTemplate from SandboxTemplate when it creates the Sandbox; it does not update an existing
// Sandbox when the template changes. The core Sandbox controller likewise leaves an existing Pod
// spec untouched—see upstream TODO: https://github.com/kubernetes-sigs/agent-sandbox/blob/059575c213eec95641c9229da8fd4525cb919617/controllers/sandbox_controller.go#L601
// Once/if the upstream fixes/implements it, then we can remove this resync logic.
const sandboxTemplateResourceVersionAnnotation = "kagent.dev/last-synced-sandbox-template-resource-version"

func sandboxTemplateNameForAgent(agentName string) string {
	return fmt.Sprintf("kagent-%s", agentName)
}

func (a *kagentReconciler) resyncAgentSandboxWorkload(ctx context.Context, sa *v1alpha2.SandboxAgent) error {
	if a.sandboxBackend == nil {
		return nil
	}

	tmplKey := types.NamespacedName{Namespace: sa.Namespace, Name: sandboxTemplateNameForAgent(sa.Name)}
	tmpl := &extensionsv1alpha1.SandboxTemplate{}
	if err := a.kube.Get(ctx, tmplKey, tmpl); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get SandboxTemplate for workload resync: %w", err)
	}
	wantRV := tmpl.ResourceVersion

	claimKey := types.NamespacedName{Namespace: sa.Namespace, Name: sa.Name}
	claim := &extensionsv1alpha1.SandboxClaim{}
	if err := a.kube.Get(ctx, claimKey, claim); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get SandboxClaim for workload resync: %w", err)
	}

	stored := ""
	if claim.Annotations != nil {
		stored = claim.Annotations[sandboxTemplateResourceVersionAnnotation]
	}
	if wantRV == stored {
		return nil
	}

	sb := &agentsandboxv1.Sandbox{}
	if err := a.kube.Get(ctx, claimKey, sb); err == nil {
		if err := a.kube.Delete(ctx, sb); err != nil {
			return fmt.Errorf("delete Sandbox %s/%s to apply updated SandboxTemplate: %w", sb.Namespace, sb.Name, err)
		}
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get Sandbox for workload resync: %w", err)
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		fresh := &extensionsv1alpha1.SandboxClaim{}
		if err := a.kube.Get(ctx, claimKey, fresh); err != nil {
			return fmt.Errorf("get SandboxClaim for sync annotation: %w", err)
		}
		base := fresh.DeepCopy()
		if fresh.Annotations == nil {
			fresh.Annotations = make(map[string]string)
		}
		fresh.Annotations[sandboxTemplateResourceVersionAnnotation] = wantRV
		return a.kube.Patch(ctx, fresh, client.MergeFrom(base))
	})
}
