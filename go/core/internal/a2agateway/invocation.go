package a2agateway

import (
	"context"
	"errors"
	"iter"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
)

func subscribeTask(ctx context.Context, client *a2aclient.Client, req *a2atype.SubscribeToTaskRequest) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		for event, err := range client.SubscribeToTask(ctx, req) {
			if errors.Is(err, a2atype.ErrTaskNotFound) {
				// A finished task may have no live stream after a controller restart.
				// Recover its final state through the subscription's normal ingester.
				task, taskErr := client.GetTask(ctx, &a2atype.GetTaskRequest{ID: req.ID})
				yield(task, taskErr)
				return
			}
			if !yield(event, err) {
				return
			}
		}
	}
}
