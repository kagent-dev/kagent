package a2agateway

import (
	"context"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

func (g *Gateway) refreshTask(ctx context.Context, instance *apiv1alpha1.AgentInstance, task *a2atype.Task) (*a2atype.Task, error) {
	client, err := g.dialer.Dial(ctx, instance)
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "failed to connect to agent instance runtime", "error", err, "instance_id", instance.GetId())
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to connect to AgentInstance runtime")
	}
	defer client.Destroy()
	result, err := client.GetTask(ctx, &a2atype.GetTaskRequest{ID: task.ID})
	if err != nil {
		return nil, err
	}
	return g.recordResult(ctx, instance, task, result, client)
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
