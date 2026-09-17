package client

// ClientOption represents a configuration option for a client set.
type ClientOption func(*baseClient)

// WithUserID sets a default user ID for requests
func WithUserID(userID string) ClientOption {
	return func(c *baseClient) {
		c.userID = userID
	}
}

type baseClient struct {
	userID    string
	transport *grpcTransport
}

func newBaseClient(rawURL string, options ...ClientOption) (*baseClient, error) {
	transport, err := newGRPCTransport(rawURL)
	if err != nil {
		return nil, err
	}
	client := &baseClient{transport: transport}

	for _, option := range options {
		option(client)
	}

	return client, nil
}
