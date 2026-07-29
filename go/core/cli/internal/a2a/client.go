package a2a

import (
	"context"
	"iter"
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
		{
			URL:             baseURL,
			ProtocolVersion: a2atype.Version,
			ProtocolBinding: a2atype.TransportProtocolJSONRPC,
		},
	}

	return a2aclient.NewFromEndpoints(
		ctx,
		endpoints,
		a2aclient.WithJSONRPCTransport(httpClient),
		a2aclient.WithCallInterceptors(corea2a.NewStaticHeadersInterceptor(headers)),
	)
}

// StreamResult contains either an A2A event or the terminal stream error.
type StreamResult struct {
	Event a2atype.Event
	Err   error
}

// StreamToChannel adapts a streaming A2A response to a channel for CLI and TUI consumption.
func StreamToChannel(
	ctx context.Context,
	client *a2aclient.Client,
	req *a2atype.SendMessageRequest,
) <-chan StreamResult {
	return streamToChannel(ctx, client.SendStreamingMessage(ctx, req))
}

func streamToChannel(
	ctx context.Context,
	stream iter.Seq2[a2atype.Event, error],
) <-chan StreamResult {
	ch := make(chan StreamResult, 1)
	go func() {
		defer close(ch)
		for event, err := range stream {
			if event == nil && err == nil {
				continue
			}

			result := StreamResult{Event: event, Err: err}
			select {
			case ch <- result:
			case <-ctx.Done():
				// Preserve a terminal iterator error when a consumer is still
				// waiting, including context cancellation and deadlines.
				if err != nil {
					select {
					case ch <- result:
					default:
					}
				}
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return ch
}
