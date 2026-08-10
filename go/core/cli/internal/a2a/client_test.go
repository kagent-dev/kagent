package a2a

import (
	"context"
	"errors"
	"iter"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/require"
)

func TestStreamToChannelForwardsEventsAndTerminalError(t *testing.T) {
	wantEvent := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("hello"))
	wantErr := errors.New("stream disconnected")
	stream := func(yield func(a2atype.Event, error) bool) {
		if !yield(wantEvent, nil) {
			return
		}
		yield(nil, wantErr)
	}

	results := collectStreamResults(streamToChannel(t.Context(), iter.Seq2[a2atype.Event, error](stream)))

	require.Len(t, results, 2)
	require.Same(t, wantEvent, results[0].Event)
	require.NoError(t, results[0].Err)
	require.Nil(t, results[1].Event)
	require.ErrorIs(t, results[1].Err, wantErr)
}

func TestStreamToChannelForwardsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	stream := func(yield func(a2atype.Event, error) bool) {
		yield(nil, ctx.Err())
	}

	results := collectStreamResults(streamToChannel(ctx, iter.Seq2[a2atype.Event, error](stream)))

	require.Len(t, results, 1)
	require.ErrorIs(t, results[0].Err, context.Canceled)
}

func collectStreamResults(ch <-chan StreamResult) []StreamResult {
	var results []StreamResult
	for result := range ch {
		results = append(results, result)
	}
	return results
}
