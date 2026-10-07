package client

// ClientOption represents a configuration option for a client set.
type ClientOption func(*baseClient)

// WithUserID sets a default user ID for requests
func WithUserID(userID string) ClientOption {
	return func(c *baseClient) {
		c.userID = userID
	}
}

// WithBearerToken sets the caller's bearer token, sent as the authorization
// metadata of every call. A server in trusted-proxy mode identifies the caller
// by it; the user ID alone only selects the data partition of an insecure one.
func WithBearerToken(token string) ClientOption {
	return func(c *baseClient) {
		c.bearerToken = token
	}
}

type baseClient struct {
	userID      string
	bearerToken string
	transport   *grpcTransport
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
