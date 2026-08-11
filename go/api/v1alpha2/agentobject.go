package v1alpha2

type WorkloadMode string

const (
	WorkloadModeSandbox WorkloadMode = "sandbox"
)

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
