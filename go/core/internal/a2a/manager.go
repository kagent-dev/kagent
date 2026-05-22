package a2a

import (
	"context"
	"fmt"
	"iter"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2aclient "github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

type PassthroughExecutor struct {
	client *a2aclient.Client
}

func NewPassthroughExecutor(client *a2aclient.Client) a2asrv.AgentExecutor {
	return &PassthroughExecutor{
		client: client,
	}
}

func (m *PassthroughExecutor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		if execCtx.Message == nil {
			yield(nil, fmt.Errorf("missing message in executor context"))
			return
		}
		req := &a2atype.SendMessageRequest{Message: execCtx.Message}
		for event, err := range m.client.SendStreamingMessage(ctx, req) {
			if !yield(event, err) {
				return
			}
		}
	}
}

func (m *PassthroughExecutor) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		task, err := m.client.CancelTask(ctx, &a2atype.CancelTaskRequest{ID: execCtx.TaskID})
		if err != nil {
			yield(nil, err)
			return
		}
		yield(task, nil)
	}
}
