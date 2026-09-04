package client

// ClientSet contains all the sub-clients for different resource types
type ClientSet struct {
	baseClient *BaseClient

	Version       Version
	AgentInstance *AgentInstanceClient
	A2A           *A2AClient
}

// New creates a new KAgent client set
func New(baseURL string, options ...ClientOption) *ClientSet {
	baseClient := NewBaseClient(baseURL, options...)

	return &ClientSet{
		baseClient:    baseClient,
		Version:       NewVersionClient(baseClient),
		AgentInstance: NewAgentInstanceClient(baseClient),
		A2A:           NewA2AClient(baseClient),
	}
}

// Close releases transport resources owned by the client set.
func (c *ClientSet) Close() error {
	if c == nil || c.baseClient == nil {
		return nil
	}
	return c.baseClient.Close()
}
