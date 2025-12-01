package client

// ClientSet contains all the sub-clients for different resource types
type ClientSet struct {
	baseClient *BaseClient

	Health      Health
	Version     Version
	ModelConfig ModelConfigInterface
	Session     Session
	Agent       Agent
	Tool        Tool
	ToolServer  ToolServer
	Memory      Memory
	Provider    Provider
	Model       Model
	Namespace   Namespace
	Feedback    Feedback
}

// New creates a new KAgent client set
func New(baseURL string, options ...ClientOption) *ClientSet {
	baseClient := NewBaseClient(baseURL, options...)

	return &ClientSet{
		baseClient:  baseClient,
		Health:      NewHealthClient(baseClient),
		Version:     NewVersionClient(baseClient),
		ModelConfig: NewModelConfigClient(baseClient),
		Session:     NewSessionClient(baseClient),
		Agent:       NewAgentClient(baseClient),
		Tool:        NewToolClient(baseClient),
		ToolServer:  NewToolServerClient(baseClient),
		Memory:      NewMemoryClient(baseClient),
		Provider:    NewProviderClient(baseClient),
		Model:       NewModelClient(baseClient),
		Namespace:   NewNamespaceClient(baseClient),
		Feedback:    NewFeedbackClient(baseClient),
	}
}
