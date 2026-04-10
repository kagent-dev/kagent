package controller

import (
	"slices"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type agentDependencyPredicate func(v1alpha2.AgentObject, types.NamespacedName) bool

func collectAgentRefs(items []v1alpha2.Agent, pred func(v1alpha2.AgentObject) bool) []types.NamespacedName {
	var out []types.NamespacedName
	for i := range items {
		agent := &items[i]
		if pred(agent) {
			out = append(out, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace})
		}
	}
	return out
}

func collectSandboxAgentRefs(items []v1alpha2.SandboxAgent, pred func(v1alpha2.AgentObject) bool) []types.NamespacedName {
	var out []types.NamespacedName
	for i := range items {
		agent := &items[i]
		if pred(agent) {
			out = append(out, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace})
		}
	}
	return out
}

func reconcileRequestsForRefs(refs []types.NamespacedName) []reconcile.Request {
	requests := make([]reconcile.Request, 0, len(refs))
	for _, ref := range refs {
		requests = append(requests, reconcile.Request{NamespacedName: ref})
	}
	return requests
}

func usesMCPServer(agent v1alpha2.AgentObject, obj types.NamespacedName) bool {
	spec := agent.GetAgentSpec()
	if spec.Type != v1alpha2.AgentType_Declarative || spec.Declarative == nil {
		return false
	}

	return slices.ContainsFunc(spec.Declarative.Tools, func(tool *v1alpha2.Tool) bool {
		return tool != nil &&
			tool.McpServer != nil &&
			tool.McpServer.ApiGroup == "kagent.dev" &&
			tool.McpServer.Kind == "MCPServer" &&
			tool.McpServer.NamespacedName(agent.GetNamespace()) == obj
	})
}

func usesRemoteMCPServer(agent v1alpha2.AgentObject, obj types.NamespacedName) bool {
	spec := agent.GetAgentSpec()
	if spec.Type != v1alpha2.AgentType_Declarative || spec.Declarative == nil {
		return false
	}

	return slices.ContainsFunc(spec.Declarative.Tools, func(tool *v1alpha2.Tool) bool {
		return tool != nil && tool.McpServer != nil && tool.McpServer.NamespacedName(agent.GetNamespace()) == obj
	})
}

func usesMCPService(agent v1alpha2.AgentObject, obj types.NamespacedName) bool {
	spec := agent.GetAgentSpec()
	if spec.Type != v1alpha2.AgentType_Declarative || spec.Declarative == nil {
		return false
	}

	return slices.ContainsFunc(spec.Declarative.Tools, func(tool *v1alpha2.Tool) bool {
		return tool != nil &&
			tool.McpServer != nil &&
			tool.McpServer.ApiGroup == "" &&
			tool.McpServer.Kind == "Service" &&
			tool.McpServer.NamespacedName(agent.GetNamespace()) == obj
	})
}

func usesModelConfig(agent v1alpha2.AgentObject, obj types.NamespacedName) bool {
	spec := agent.GetAgentSpec()
	return agent.GetNamespace() == obj.Namespace &&
		spec.Type == v1alpha2.AgentType_Declarative &&
		spec.Declarative != nil &&
		spec.Declarative.ModelConfig == obj.Name
}

func referencesConfigMap(agent v1alpha2.AgentObject, obj types.NamespacedName) bool {
	spec := agent.GetAgentSpec()
	if agent.GetNamespace() != obj.Namespace || spec.Type != v1alpha2.AgentType_Declarative || spec.Declarative == nil {
		return false
	}

	if ref := spec.Declarative.SystemMessageFrom; ref != nil {
		if ref.Type == v1alpha2.ConfigMapValueSource && ref.Name == obj.Name {
			return true
		}
	}

	if pt := spec.Declarative.PromptTemplate; pt != nil {
		return slices.ContainsFunc(pt.DataSources, func(ds v1alpha2.PromptSource) bool {
			return ds.Name == obj.Name
		})
	}

	return false
}
