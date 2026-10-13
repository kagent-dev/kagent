package translator

import (
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TemplateConfiguration is reusable behavior resolved for compilation.
// Name identifies this node for diagnostics and prompt rendering. For inline
// behavior it is the owning Agent's name. Source is nil for inline configuration.
type TemplateConfiguration struct {
	Name      string
	Namespace string
	Spec      v1alpha3.AgentTemplateSpec
	Source    *metav1.ObjectMeta
}

// HarnessConfiguration is execution configuration resolved from a referenced
// Harness for compilation.
type HarnessConfiguration struct {
	Name      string
	Namespace string
	Spec      v1alpha3.HarnessSpec
	Source    metav1.ObjectMeta
}

func templateConfiguration(resource *v1alpha3.AgentTemplate) *TemplateConfiguration {
	return &TemplateConfiguration{Name: resource.Name, Namespace: resource.Namespace,
		Spec: *resource.Spec.DeepCopy(), Source: resource.ObjectMeta.DeepCopy()}
}

func harnessConfiguration(resource *v1alpha3.Harness) *HarnessConfiguration {
	return &HarnessConfiguration{Name: resource.Name, Namespace: resource.Namespace,
		Spec: *resource.Spec.DeepCopy(), Source: *resource.ObjectMeta.DeepCopy()}
}
