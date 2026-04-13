package v1alpha2

import "sigs.k8s.io/controller-runtime/pkg/client"

type WorkloadMode string

const (
	WorkloadModeDeployment WorkloadMode = "deployment"
	WorkloadModeSandbox    WorkloadMode = "sandbox"
)

// AgentObject is the shared shape implemented by agent-style CRDs that expose the
// same Spec/Status model but reconcile to different workload types.
// +kubebuilder:object:generate=false
type AgentObject interface {
	client.Object
	GetAgentSpec() *AgentSpec
	GetAgentStatus() *AgentStatus
	GetWorkloadMode() WorkloadMode
}

func (a *Agent) GetAgentSpec() *AgentSpec {
	if a == nil {
		return nil
	}
	return &a.Spec
}

func (a *Agent) GetAgentStatus() *AgentStatus {
	if a == nil {
		return nil
	}
	return &a.Status
}

func (a *Agent) GetWorkloadMode() WorkloadMode {
	return WorkloadModeDeployment
}

func (a *SandboxAgent) GetAgentSpec() *AgentSpec {
	if a == nil {
		return nil
	}
	return &a.Spec
}

func (a *SandboxAgent) GetAgentStatus() *AgentStatus {
	if a == nil {
		return nil
	}
	return &a.Status
}

func (a *SandboxAgent) GetWorkloadMode() WorkloadMode {
	return WorkloadModeSandbox
}
