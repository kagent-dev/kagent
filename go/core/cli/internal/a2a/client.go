package a2a

import (
	"context"
	"fmt"
	"net/http"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2aclient "github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	corea2a "github.com/kagent-dev/kagent/go/core/internal/a2a"
)

// ClientOptions configures an official A2A v1 client for CLI use.
type ClientOptions struct {
	HTTPClient *http.Client
	Headers    map[string]string
	Timeout    time.Duration
}

// NewClient resolves an agent card and returns an A2A v1 client that sends A2A-Version: 1.0.
func NewClient(ctx context.Context, baseURL string, opts ClientOptions) (*a2aclient.Client, error) {
	headers := make(map[string]string, len(opts.Headers)+1)
	for k, v := range opts.Headers {
		headers[k] = v
	}
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

	resolver := agentcard.NewResolver(httpClient)
	resolveOpts := make([]agentcard.ResolveOption, 0, len(headers))
	for k, v := range headers {
		resolveOpts = append(resolveOpts, agentcard.WithRequestHeader(k, v))
	}

	card, err := resolver.Resolve(ctx, baseURL, resolveOpts...)
	if err != nil {
		return nil, fmt.Errorf("resolve agent card at %s: %w", baseURL, err)
	}

	client, err := a2aclient.NewFromCard(
		ctx,
		card,
		a2aclient.WithJSONRPCTransport(httpClient),
		a2aclient.WithCallInterceptors(corea2a.NewStaticHeadersInterceptor(headers)),
	)
	if err != nil {
		return nil, fmt.Errorf("create A2A client for %s: %w", baseURL, err)
	}

	return client, nil
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
