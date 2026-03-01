package a2a

import (
	"context"
	"iter"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2asrv"
)

// PassthroughHandler implements a2asrv.RequestHandler by delegating all calls
// to the downstream a2aclient.Client. This acts as a proxy between the
// incoming A2A server requests and the backend agent's A2A client.
type PassthroughHandler struct {
	client *a2aclient.Client
}

var _ a2asrv.RequestHandler = (*PassthroughHandler)(nil)

func NewPassthroughHandler(client *a2aclient.Client) a2asrv.RequestHandler {
	return &PassthroughHandler{
		client: client,
	}
}

func (h *PassthroughHandler) OnSendMessage(ctx context.Context, params *a2a.MessageSendParams) (a2a.SendMessageResult, error) {
	return h.client.SendMessage(ctx, params)
}

func (h *PassthroughHandler) OnSendMessageStream(ctx context.Context, params *a2a.MessageSendParams) iter.Seq2[a2a.Event, error] {
	return h.client.SendStreamingMessage(ctx, params)
}

func (h *PassthroughHandler) OnGetTask(ctx context.Context, query *a2a.TaskQueryParams) (*a2a.Task, error) {
	return h.client.GetTask(ctx, query)
}

func (h *PassthroughHandler) OnListTasks(ctx context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	return h.client.ListTasks(ctx, req)
}

func (h *PassthroughHandler) OnCancelTask(ctx context.Context, params *a2a.TaskIDParams) (*a2a.Task, error) {
	return h.client.CancelTask(ctx, params)
}

func (h *PassthroughHandler) OnGetTaskPushConfig(ctx context.Context, params *a2a.GetTaskPushConfigParams) (*a2a.TaskPushConfig, error) {
	return h.client.GetTaskPushConfig(ctx, params)
}

func (h *PassthroughHandler) OnListTaskPushConfig(ctx context.Context, params *a2a.ListTaskPushConfigParams) ([]*a2a.TaskPushConfig, error) {
	return h.client.ListTaskPushConfig(ctx, params)
}

func (h *PassthroughHandler) OnSetTaskPushConfig(ctx context.Context, params *a2a.TaskPushConfig) (*a2a.TaskPushConfig, error) {
	return h.client.SetTaskPushConfig(ctx, params)
}

func (h *PassthroughHandler) OnDeleteTaskPushConfig(ctx context.Context, params *a2a.DeleteTaskPushConfigParams) error {
	return h.client.DeleteTaskPushConfig(ctx, params)
}

func (h *PassthroughHandler) OnResubscribeToTask(ctx context.Context, params *a2a.TaskIDParams) iter.Seq2[a2a.Event, error] {
	return h.client.ResubscribeToTask(ctx, params)
}

func (h *PassthroughHandler) OnGetExtendedAgentCard(ctx context.Context) (*a2a.AgentCard, error) {
	return h.client.GetAgentCard(ctx)
}
