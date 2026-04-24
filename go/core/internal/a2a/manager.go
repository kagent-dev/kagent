package a2a

import (
	"context"
	"sync"

	"trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
	"trpc.group/trpc-go/trpc-a2a-go/taskmanager"
)

// TargetURLResolverFn resolves a per-request A2A target URL from the conversation context ID.
// It returns (url, true, nil) when a dynamic target is found for the given contextID,
// or ("", false, nil) to fall back to the statically-configured agent URL.
type TargetURLResolverFn func(ctx context.Context, contextID string) (url string, ok bool, err error)

type PassthroughManager struct {
	client     *client.A2AClient
	resolver   TargetURLResolverFn
	dynClients sync.Map // url string → *client.A2AClient; avoids creating a new client per request
}

func NewPassthroughManager(c *client.A2AClient, resolver TargetURLResolverFn) taskmanager.TaskManager {
	return &PassthroughManager{
		client:   c,
		resolver: resolver,
	}
}

// resolveClient returns a client targeting the dynamically resolved URL when the resolver
// finds a route for contextID. Falls back to the static client when contextID is empty,
// the resolver is nil, or no route is found. Clients are cached by URL so the underlying
// HTTP connection pool is reused across requests.
func (m *PassthroughManager) resolveClient(ctx context.Context, contextID *string) *client.A2AClient {
	if m.resolver == nil || contextID == nil || *contextID == "" {
		return m.client
	}
	url, ok, err := m.resolver(ctx, *contextID)
	if err != nil || !ok {
		return m.client
	}
	if cached, loaded := m.dynClients.Load(url); loaded {
		return cached.(*client.A2AClient)
	}
	dynClient, err := client.NewA2AClient(url)
	if err != nil {
		return m.client
	}
	actual, _ := m.dynClients.LoadOrStore(url, dynClient)
	return actual.(*client.A2AClient)
}

func (m *PassthroughManager) OnSendMessage(ctx context.Context, request protocol.SendMessageParams) (*protocol.MessageResult, error) {
	if request.Message.MessageID == "" {
		request.Message.MessageID = protocol.GenerateMessageID()
	}
	if request.Message.Kind == "" {
		request.Message.Kind = protocol.KindMessage
	}
	return m.resolveClient(ctx, request.Message.ContextID).SendMessage(ctx, request)
}

func (m *PassthroughManager) OnSendMessageStream(ctx context.Context, request protocol.SendMessageParams) (<-chan protocol.StreamingMessageEvent, error) {
	if request.Message.MessageID == "" {
		request.Message.MessageID = protocol.GenerateMessageID()
	}
	if request.Message.Kind == "" {
		request.Message.Kind = protocol.KindMessage
	}
	return m.resolveClient(ctx, request.Message.ContextID).StreamMessage(ctx, request)
}

func (m *PassthroughManager) OnGetTask(ctx context.Context, params protocol.TaskQueryParams) (*protocol.Task, error) {
	return m.client.GetTasks(ctx, params)
}

func (m *PassthroughManager) OnCancelTask(ctx context.Context, params protocol.TaskIDParams) (*protocol.Task, error) {
	return m.client.CancelTasks(ctx, params)
}

func (m *PassthroughManager) OnPushNotificationSet(ctx context.Context, params protocol.TaskPushNotificationConfig) (*protocol.TaskPushNotificationConfig, error) {
	return m.client.SetPushNotification(ctx, params)
}

func (m *PassthroughManager) OnPushNotificationGet(ctx context.Context, params protocol.TaskIDParams) (*protocol.TaskPushNotificationConfig, error) {
	return m.client.GetPushNotification(ctx, params)
}

func (m *PassthroughManager) OnResubscribe(ctx context.Context, params protocol.TaskIDParams) (<-chan protocol.StreamingMessageEvent, error) {
	return m.client.ResubscribeTask(ctx, params)
}
