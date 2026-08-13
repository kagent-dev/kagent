package client

// ClientSet contains all the sub-clients for different resource types
type ClientSet struct {
	baseClient *BaseClient

	Health              Health
	Version             Version
	ModelConfig         ModelConfigInterface
	Session             Session
	Agent               Agent
	Tool                Tool
	ToolServer          ToolServer
	ModelProviderConfig ModelProviderConfig
	Model               Model
	Namespace           Namespace
	Feedback            Feedback
}

// New creates a new KAgent client set
func New(baseURL string, options ...ClientOption) *ClientSet {
	baseClient := NewBaseClient(baseURL, options...)

	return &ClientSet{
		baseClient:          baseClient,
		Health:              NewHealthClient(baseClient),
		Version:             NewVersionClient(baseClient),
		ModelConfig:         NewModelConfigClient(baseClient),
		Session:             NewSessionClient(baseClient),
		Agent:               NewAgentClient(baseClient),
		Tool:                NewToolClient(baseClient),
		ToolServer:          NewToolServerClient(baseClient),
		ModelProviderConfig: NewModelProviderConfigClient(baseClient),
		Model:               NewModelClient(baseClient),
		Namespace:           NewNamespaceClient(baseClient),
		Feedback:            NewFeedbackClient(baseClient),
	}
}

// Close releases transport resources owned by the client set.
func (c *ClientSet) Close() error {
	if c == nil || c.baseClient == nil {
		return nil
	}
	return c.baseClient.Close()
}
