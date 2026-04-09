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
)

// Backend builds kubernetes-sigs/agent-sandbox Sandbox CRs directly (no SandboxTemplate/SandboxClaim).
type Backend struct{}

var _ sandboxbackend.Backend = (*Backend)(nil)

// New returns the agent-sandbox backend.
func New() *Backend {
	return &Backend{}
}

func (b *Backend) GetOwnedResourceTypes() []client.Object {
	return []client.Object{
		&agentsandboxv1.Sandbox{},
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

	pt := agentsandboxv1.PodTemplate{
		Spec: in.PodTemplate.Spec,
		ObjectMeta: agentsandboxv1.PodMetadata{
			Labels:      podLabels,
			Annotations: in.PodTemplate.Annotations,
		},
	}

	labelUnion := mapsUnion(podLabels, in.Agent.Labels)

	replicas := int32(1)
	sb := &agentsandboxv1.Sandbox{
		TypeMeta: metav1.TypeMeta{
			APIVersion: agentsandboxv1.GroupVersion.String(),
			Kind:       "Sandbox",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   in.Agent.Namespace,
			Annotations: in.Agent.Annotations,
			Labels:      labelUnion,
		},
		Spec: agentsandboxv1.SandboxSpec{
			PodTemplate: pt,
			Replicas:    &replicas,
		},
	}

	return []client.Object{sb}, nil
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
