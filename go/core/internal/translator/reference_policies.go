package translator

import (
	"istio.io/istio/pkg/kube/krt"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// agentReferencePolicies collects policies independently of configuration validity.
// Missing inputs are left to normal resolution; they must not hide policies on
// other inputs. Fetch every reachable reference to retain KRT dependencies even
// while compilation fails. The visited set also bounds invalid template cycles.
func agentReferencePolicies(ctx krt.HandlerContext, inputs Collections, agent *v1alpha3.Agent) []*v1alpha3.AllowedNamespaces {
	var policies []*v1alpha3.AllowedNamespaces
	addModel := func(ref *v1alpha3.ObjectReference) {
		if ref == nil {
			return
		}
		key := ref.NamespacedName(agent.Namespace)
		model := krt.FetchOne(ctx, inputs.ResolvedModelConfigs, krt.FilterObjectName(key))
		if model != nil {
			policies = append(policies, model.Config.Spec.AllowedNamespaces)
		}
	}
	if harness := krt.FetchOne(ctx, inputs.Harnesses, krt.FilterObjectName(types.NamespacedName{Namespace: agent.Namespace, Name: agent.Spec.HarnessRef.Name})); harness != nil && (*harness).Spec.Kagent != nil {
		spec := (*harness).Spec.Kagent
		if spec.Memory != nil {
			addModel(&spec.Memory.ModelConfigRef)
		}
		if spec.Compaction != nil && spec.Compaction.Summarizer != nil {
			addModel(spec.Compaction.Summarizer.ModelConfigRef)
		}
	}
	seen := map[string]bool{}
	var visitTemplate func(*v1alpha3.AgentTemplateSpec)
	visitNamed := func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		template := krt.FetchOne(ctx, inputs.AgentTemplates, krt.FilterObjectName(types.NamespacedName{Namespace: agent.Namespace, Name: name}))
		if template != nil {
			visitTemplate(&(*template).Spec)
		}
	}
	visitTemplate = func(spec *v1alpha3.AgentTemplateSpec) {
		addModel(spec.ModelConfig)
		for _, tool := range spec.Tools {
			if tool.MCP != nil && tool.MCP.Server.Kind == "RemoteMCPServer" {
				key := (v1alpha3.ObjectReference{Name: tool.MCP.Server.Name, Namespace: ptr.Deref(tool.MCP.Server.Namespace, "")}).NamespacedName(agent.Namespace)
				server := krt.FetchOne(ctx, inputs.RemoteMCPServers, krt.FilterObjectName(key))
				if server != nil {
					policies = append(policies, (*server).Spec.AllowedNamespaces)
				}
			}
			if tool.SubAgent != nil && tool.SubAgent.TemplateRef != nil {
				visitNamed(tool.SubAgent.TemplateRef.Name)
			}
		}
	}
	if agent.Spec.Template.Inline != nil {
		visitTemplate(agent.Spec.Template.Inline)
	}
	if agent.Spec.Template.Ref != nil {
		visitNamed(agent.Spec.Template.Ref.Name)
	}
	return policies
}

// AgentNeedsNamespace reports whether any referenced model or MCP policy needs
// source namespace labels, including auxiliary models and subagent references.
func AgentNeedsNamespace(ctx krt.HandlerContext, inputs Collections, agent *v1alpha3.Agent) bool {
	policies := agentReferencePolicies(ctx, inputs, agent)
	for _, policy := range policies {
		if policy != nil && policy.From == v1alpha3.NamespacesFromSelector {
			return true
		}
	}
	return false
}
