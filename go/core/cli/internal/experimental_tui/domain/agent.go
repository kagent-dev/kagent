package domain

import "time"

type AgentStatus string

const (
	AgentStatusRunning  AgentStatus = "running"
	AgentStatusStopped  AgentStatus = "stopped"
	AgentStatusError    AgentStatus = "error"
	AgentStatusFinished AgentStatus = "finished"
)

type Agent struct {
	ID           string
	Name         string
	Status       AgentStatus
	RestartCount int
	UpdatedAt    time.Time
	CreatedAt    time.Time

	Provider ModelProvider
}

func NewAgent(id, name string, status AgentStatus, provider ModelProvider) Agent {
	now := time.Now()

	return Agent{
		ID:           id,
		Name:         name,
		Status:       status,
		Provider:     provider,
		RestartCount: 0,
		UpdatedAt:    now,
		CreatedAt:    now,
	}
}

func (a Agent) WithProvider(provider ModelProvider) Agent {
	a.Provider = provider
	a.UpdatedAt = time.Now()
	return a
}

func (a Agent) WithStatus(status AgentStatus) Agent {
	a.Status = status
	a.UpdatedAt = time.Now()
	return a
}

func (a Agent) TotalCost() float64 {
	// TODO: Implement cost calculation based on the provider's token usage and cost per token.
	return 0.
}

func (a Agent) ContextUsageRate() float64 {
	max := a.Provider.MaxContextTokens()
	if max == 0 {
		return 0
	}

	current := float64(a.Provider.InputTokens() + a.Provider.OutputTokens())
	return current / float64(max)
}

func (a Agent) TokenRatio() (int, int) {
	return a.Provider.InputTokens(), a.Provider.OutputTokens()
}

func (a Agent) LastResponseTime() time.Duration {
	// TODO: Implement logic to track the time taken for the last response from the model provider.
	return 0
}

func (a Agent) Throughput() float64 {
	// TODO: Implement logic to calculate the number of tokens processed per second.
	return 0
}

func (a Agent) Latency() time.Duration {
	// TODO: Implement logic to determine if the agent is experiencing latency issues based on response times and throughput.
	return 0
}

func (a Agent) TotalSuccessRate() float64 {
	// TODO: Implement logic to calculate the total success rate of the agent's operations.
	return 0
}

func (a Agent) ThoughtSteps() int {
	// TODO: Implement logic to count the number of thought steps taken by the agent, if supported by the provider.
	return 0
}

func (a Agent) Model() string {
	return a.Provider.Name()
}

func (a Agent) Uptime() time.Duration {
	return time.Since(a.CreatedAt)
}
