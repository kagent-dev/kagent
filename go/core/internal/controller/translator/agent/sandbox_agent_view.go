package agent

import (
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AgentViewFromSandboxAgent returns an in-memory Agent with the same spec, metadata, and status
// as the SandboxAgent. Use with TranslateAgent(ctx, view, true) so the translator emits sandbox
// workload objects; the returned value is not persisted as an Agent resource.
func AgentViewFromSandboxAgent(sa *v1alpha2.SandboxAgent) *v1alpha2.Agent {
	if sa == nil {
		return nil
	}
	spec := v1alpha2.AgentSpec{
		Type:              sa.Spec.Type,
		Declarative:       sa.Spec.Declarative,
		BYO:               sa.Spec.BYO,
		Description:       sa.Spec.Description,
		Skills:            sa.Spec.Skills,
		AllowedNamespaces: sa.Spec.AllowedNamespaces,
	}
	a := &v1alpha2.Agent{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1alpha2.GroupVersion.String(),
			Kind:       "Agent",
		},
		ObjectMeta: sa.ObjectMeta,
		Spec:       spec,
	}
	sa.Status.DeepCopyInto(&a.Status)
	return a
}
