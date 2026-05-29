package a2a

import (
	"context"
	"maps"
	"net/http"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2aclient "github.com/a2aproject/a2a-go/v2/a2aclient"
	corea2a "github.com/kagent-dev/kagent/go/core/internal/a2a"
)

// ClientOptions configures an official A2A v1 client for CLI use.
type ClientOptions struct {
	HTTPClient *http.Client
	Headers    map[string]string
	Timeout    time.Duration
}

// NewClient returns an A2A v1 client pointed directly at baseURL without resolving
// the agent card. The card's published URL contains the in-cluster hostname which is
// unreachable from outside the cluster; skipping resolution mirrors the pre-v1 behaviour
// of constructing the endpoint URL directly from the user's config.
func NewClient(ctx context.Context, baseURL string, opts ClientOptions) (*a2aclient.Client, error) {
	headers := make(map[string]string, len(opts.Headers)+1)
	maps.Copy(headers, opts.Headers)
	if _, ok := headers[a2atype.SvcParamVersion]; !ok {
		headers[a2atype.SvcParamVersion] = string(a2atype.Version)
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		timeout := opts.Timeout
		if timeout == 0 {
			timeout = 60 * time.Second
		}
		httpClient = &http.Client{Timeout: timeout}
	}

	endpoints := []*a2atype.AgentInterface{
		{URL: baseURL, ProtocolBinding: a2atype.TransportProtocolJSONRPC},
	}

	return a2aclient.NewFromEndpoints(
		ctx,
		endpoints,
		a2aclient.WithJSONRPCTransport(httpClient),
		a2aclient.WithCallInterceptors(corea2a.NewStaticHeadersInterceptor(headers)),
	)
}

// StreamToChannel adapts a streaming A2A response to a channel for TUI consumption.
func StreamToChannel(ctx context.Context, client *a2aclient.Client, req *a2atype.SendMessageRequest) (<-chan a2atype.Event, error) {
	ch := make(chan a2atype.Event)
	go func() {
		defer close(ch)
		for event, err := range client.SendStreamingMessage(ctx, req) {
			if err != nil {
				return
			}
			if event != nil {
				select {
				case ch <- event:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return ch, nil
}

// V1RequestHeaders returns HTTP headers that select official A2A v1 wire format.
func V1RequestHeaders() map[string]string {
	return map[string]string{
		a2atype.SvcParamVersion: string(a2atype.Version),
	}
}
