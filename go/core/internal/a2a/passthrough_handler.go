package a2a

import (
	"context"
	"iter"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2aclient "github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

type PassthroughRequestHandler struct {
	client *a2aclient.Client
	card   *a2atype.AgentCard
}

var _ a2asrv.RequestHandler = (*PassthroughRequestHandler)(nil)

// NewPassthroughRequestHandler returns a transport-level proxy for controller
// A2A endpoints. It delegates each request directly to the selected upstream
// agent client and intentionally bypasses a2asrv.NewHandler, which would create
// local task state and apply v1 task-processing invariants to legacy streams.
func NewPassthroughRequestHandler(client *a2aclient.Client, card *a2atype.AgentCard) *PassthroughRequestHandler {
	return &PassthroughRequestHandler{
		client: client,
		card:   card,
	}
}

func (h *PassthroughRequestHandler) GetTask(ctx context.Context, req *a2atype.GetTaskRequest) (*a2atype.Task, error) {
	return h.client.GetTask(ctx, req)
}

func (h *PassthroughRequestHandler) ListTasks(ctx context.Context, req *a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error) {
	return h.client.ListTasks(ctx, req)
}

func (h *PassthroughRequestHandler) CancelTask(ctx context.Context, req *a2atype.CancelTaskRequest) (*a2atype.Task, error) {
	return h.client.CancelTask(ctx, req)
}

func (h *PassthroughRequestHandler) SendMessage(ctx context.Context, req *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
	return h.client.SendMessage(ctx, req)
}

func (h *PassthroughRequestHandler) SubscribeToTask(ctx context.Context, req *a2atype.SubscribeToTaskRequest) iter.Seq2[a2atype.Event, error] {
	return h.client.SubscribeToTask(ctx, req)
}

func (h *PassthroughRequestHandler) SendStreamingMessage(ctx context.Context, req *a2atype.SendMessageRequest) iter.Seq2[a2atype.Event, error] {
	return h.client.SendStreamingMessage(ctx, req)
}

func (h *PassthroughRequestHandler) GetTaskPushConfig(ctx context.Context, req *a2atype.GetTaskPushConfigRequest) (*a2atype.PushConfig, error) {
	return h.client.GetTaskPushConfig(ctx, req)
}

func (h *PassthroughRequestHandler) ListTaskPushConfigs(ctx context.Context, req *a2atype.ListTaskPushConfigRequest) (*a2atype.ListTaskPushConfigResponse, error) {
	configs, err := h.client.ListTaskPushConfigs(ctx, req)
	if err != nil {
		return nil, err
	}
	return &a2atype.ListTaskPushConfigResponse{Configs: configs}, nil
}

func (h *PassthroughRequestHandler) CreateTaskPushConfig(ctx context.Context, req *a2atype.PushConfig) (*a2atype.PushConfig, error) {
	return h.client.CreateTaskPushConfig(ctx, req)
}

func (h *PassthroughRequestHandler) DeleteTaskPushConfig(ctx context.Context, req *a2atype.DeleteTaskPushConfigRequest) error {
	return h.client.DeleteTaskPushConfig(ctx, req)
}

func (h *PassthroughRequestHandler) GetExtendedAgentCard(ctx context.Context, req *a2atype.GetExtendedAgentCardRequest) (*a2atype.AgentCard, error) {
	if h.card != nil && !h.card.Capabilities.ExtendedAgentCard {
		return h.card, nil
	}
	return h.client.GetExtendedAgentCard(ctx, req)
}
