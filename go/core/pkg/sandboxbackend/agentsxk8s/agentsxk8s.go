package agentsxk8s

import (
	"context"
	"fmt"
	"maps"

	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsandboxv1 "sigs.k8s.io/agent-sandbox/api/v1alpha1"
	extensionsv1alpha1 "sigs.k8s.io/agent-sandbox/extensions/api/v1alpha1"
)

// Backend builds kubernetes-sigs/agent-sandbox SandboxTemplate + SandboxClaim resources.
type Backend struct{}

var _ sandboxbackend.Backend = (*Backend)(nil)

// New returns the agent-sandbox backend.
func New() *Backend {
	return &Backend{}
}

func (b *Backend) GetOwnedResourceTypes() []client.Object {
	return []client.Object{
		&extensionsv1alpha1.SandboxTemplate{},
		&extensionsv1alpha1.SandboxClaim{},
	}
}

func (b *Backend) BuildSandbox(_ context.Context, in sandboxbackend.BuildInput) ([]client.Object, error) {
	if in.Agent == nil {
		return nil, fmt.Errorf("agent is required")
	}
	name := in.Agent.Name
	if in.WorkloadName != "" {
		name = in.WorkloadName
	}
	podLabels := in.PodTemplate.Labels
	if len(in.ExtraLabels) > 0 {
		podLabels = mapsUnion(podLabels, in.ExtraLabels)
	}

	return b.buildSandboxTemplateAndClaim(in, name, podLabels)
}

func (b *Backend) buildSandboxTemplateAndClaim(in sandboxbackend.BuildInput, claimName string, podLabels map[string]string) ([]client.Object, error) {
	tmplName := in.TemplateName
	if tmplName == "" {
		return nil, fmt.Errorf("template name is required")
	}

	pt := agentsandboxv1.PodTemplate{
		Spec: in.PodTemplate.Spec,
		ObjectMeta: agentsandboxv1.PodMetadata{
			Labels:      podLabels,
			Annotations: in.PodTemplate.Annotations,
		},
	}

	labelUnion := mapsUnion(podLabels, in.Agent.Labels)

	tmplSpec := extensionsv1alpha1.SandboxTemplateSpec{
		PodTemplate: pt,
		// Unmanaged allows kagent to reach the agent Service in-cluster without agent-sandbox-managed NetworkPolicies.
		NetworkPolicyManagement: extensionsv1alpha1.NetworkPolicyManagementUnmanaged,
	}

	st := &extensionsv1alpha1.SandboxTemplate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: extensionsv1alpha1.GroupVersion.String(),
			Kind:       "SandboxTemplate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        tmplName,
			Namespace:   in.Agent.Namespace,
			Annotations: in.Agent.Annotations,
			Labels:      labelUnion,
		},
		Spec: tmplSpec,
	}
	out := []client.Object{st}

	claim := &extensionsv1alpha1.SandboxClaim{
		TypeMeta: metav1.TypeMeta{
			APIVersion: extensionsv1alpha1.GroupVersion.String(),
			Kind:       "SandboxClaim",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        claimName,
			Namespace:   in.Agent.Namespace,
			Annotations: in.Agent.Annotations,
			Labels:      labelUnion,
		},
		Spec: extensionsv1alpha1.SandboxClaimSpec{
			TemplateRef: extensionsv1alpha1.SandboxTemplateRef{
				Name: tmplName,
			},
		},
	}
	out = append(out, claim)
	return out, nil
}

func mapsUnion(podLabels map[string]string, agentLabels map[string]string) map[string]string {
	if len(podLabels) == 0 && len(agentLabels) == 0 {
		return nil
	}
	out := make(map[string]string, len(podLabels)+len(agentLabels))
	maps.Copy(out, podLabels)
	for k, v := range agentLabels {
		if _, ok := out[k]; !ok {
			out[k] = v
		}
	}
	return out
}

func (b *Backend) ComputeReady(ctx context.Context, cl client.Client, nn types.NamespacedName) (metav1.ConditionStatus, string, string) {
	sb := &agentsandboxv1.Sandbox{}
	if err := cl.Get(ctx, nn, sb); err != nil {
		if apierrors.IsNotFound(err) {
			return metav1.ConditionUnknown, "SandboxNotFound", err.Error()
		}
		return metav1.ConditionUnknown, "SandboxGetFailed", err.Error()
	}
	for i := range sb.Status.Conditions {
		c := sb.Status.Conditions[i]
		if c.Type == string(agentsandboxv1.SandboxConditionReady) {
			return c.Status, c.Reason, c.Message
		}
	}
	return metav1.ConditionUnknown, "SandboxReadyPending", "Sandbox Ready condition not yet reported"
}
