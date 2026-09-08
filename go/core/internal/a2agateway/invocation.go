package a2agateway

import (
	"context"
	"errors"
	"iter"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
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

func (g *Gateway) recordResult(ctx context.Context, instance *apiv1alpha1.AgentInstance, task *a2atype.Task, result a2atype.SendMessageResult, client *a2aclient.Client) (*a2atype.Task, error) {
	release := g.coordinator.Quiesce(instance.GetId())
	defer release()
	// A cancellation or another observer may have finished while the RPC was
	// in flight. The stream ingester, when present, owns task persistence.
	latest, err := g.store.GetAgentInstanceTask(ctx, instance.GetId(), string(task.ID))
	if err != nil {
		return nil, g.storeError(ctx, err)
	}
	if _, observing := g.taskRun(instance.GetId(), task.ID); observing || isQuiescent(latest.Status.State) {
		return latest, nil
	}
	updated, err := taskForResult(latest, result)
	if err != nil {
		return nil, err
	}
	if updated.Status.Timestamp == nil {
		now := time.Now()
		updated.Status.Timestamp = &now
	}
	if isQuiescent(updated.Status.State) {
		if err := client.Destroy(); err != nil {
			return nil, err
		}
	}
	if err := g.storeEvent(ctx, instance, updated, result); err != nil {
		return nil, g.storeError(ctx, err)
	}
	return updated, nil
}
