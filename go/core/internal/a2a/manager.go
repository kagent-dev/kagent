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

// TargetHeadersResolverFn resolves per-request HTTP headers for dynamically routed A2A traffic.
type TargetHeadersResolverFn func(ctx context.Context, contextID string) (headers map[string]string, ok bool, err error)

type PassthroughManager struct {
	client          *client.A2AClient
	resolver        TargetURLResolverFn
	headersResolver TargetHeadersResolverFn
	dynClients      sync.Map // route cache key → *client.A2AClient; avoids creating a new client per request
}

func NewPassthroughManager(c *client.A2AClient, resolver TargetURLResolverFn, headersResolver TargetHeadersResolverFn) taskmanager.TaskManager {
	return &PassthroughManager{
		client:          c,
		resolver:        resolver,
		headersResolver: headersResolver,
	}
}

// resolveTarget returns a client targeting the dynamically resolved URL when the resolver
// finds a route for contextID. Falls back to the static client when contextID is empty,
// the resolver is nil, or no route is found. Clients are cached by URL so the underlying
// HTTP connection pool is reused across requests.
func (m *PassthroughManager) resolveTarget(ctx context.Context, contextID *string) (*client.A2AClient, []client.RequestOption) {
	if m.resolver == nil || contextID == nil || *contextID == "" {
		return m.client, nil
	}
	url, ok, err := m.resolver(ctx, *contextID)
	if err != nil || !ok {
		return m.client, nil
	}

	var opts []client.RequestOption
	cacheKey := url
	if m.headersResolver != nil {
		headers, ok, err := m.headersResolver(ctx, *contextID)
		if err != nil || !ok {
			return m.client, nil
		}
		if len(headers) > 0 {
			opts = append(opts, client.WithRequestHeaders(headers))
			// Preserve per-context client isolation for integrations that previously
			// encoded the context ID into the URL while still sending a generic URL on the wire.
			cacheKey = url + "\x00" + *contextID
		}
	}

	if cached, loaded := m.dynClients.Load(cacheKey); loaded {
		return cached.(*client.A2AClient), opts
	}
	dynClient, err := client.NewA2AClient(url)
	if err != nil {
		return m.client, nil
	}
	actual, _ := m.dynClients.LoadOrStore(cacheKey, dynClient)
	return actual.(*client.A2AClient), opts
}

func (m *PassthroughManager) OnSendMessage(ctx context.Context, request protocol.SendMessageParams) (*protocol.MessageResult, error) {
	if request.Message.MessageID == "" {
		request.Message.MessageID = protocol.GenerateMessageID()
	}
	if request.Message.Kind == "" {
		request.Message.Kind = protocol.KindMessage
	}
	resolvedClient, opts := m.resolveTarget(ctx, request.Message.ContextID)
	return resolvedClient.SendMessage(ctx, request, opts...)
}

func (m *PassthroughManager) OnSendMessageStream(ctx context.Context, request protocol.SendMessageParams) (<-chan protocol.StreamingMessageEvent, error) {
	if request.Message.MessageID == "" {
		request.Message.MessageID = protocol.GenerateMessageID()
	}
	if request.Message.Kind == "" {
		request.Message.Kind = protocol.KindMessage
	}
	resolvedClient, opts := m.resolveTarget(ctx, request.Message.ContextID)
	return resolvedClient.StreamMessage(ctx, request, opts...)
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
