package client

// ClientOption represents a configuration option for the client
type ClientOption func(*BaseClient)

// WithUserID sets a default user ID for requests
func WithUserID(userID string) ClientOption {
	return func(c *BaseClient) {
		c.UserID = userID
	}
}

// BaseClient contains the shared transport configuration used by all sub-clients.
type BaseClient struct {
	UserID  string // Default user ID for requests that require it
	api     *grpcTransport
	gateway *grpcTransport
}

// NewBaseClient creates a new base client with the given configuration
func NewBaseClient(apiURL, gatewayURL string, options ...ClientOption) (*BaseClient, error) {
	api, err := newGRPCTransport(apiURL)
	if err != nil {
		return nil, err
	}
	gateway, err := newGRPCTransport(gatewayURL)
	if err != nil {
		return nil, err
	}
	client := &BaseClient{
		api:     api,
		gateway: gateway,
	}

	for _, option := range options {
		option(client)
	}

	return client, nil
}
