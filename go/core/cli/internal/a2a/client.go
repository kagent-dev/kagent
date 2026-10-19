package a2a

import (
	"context"
	"iter"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2aclient "github.com/a2aproject/a2a-go/v2/a2aclient"
)

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
