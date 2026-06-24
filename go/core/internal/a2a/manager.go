package a2a

import (
	"context"
	"fmt"

	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
	"trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
	"trpc.group/trpc-go/trpc-a2a-go/taskmanager"
)

type PassthroughManager struct {
	client *client.A2AClient
}

func NewPassthroughManager(client *client.A2AClient) taskmanager.TaskManager {
	return &PassthroughManager{
		client: client,
	}
}

// validateShareContext returns an error if a share context is present but the
// given A2A contextID doesn't match the session the token was issued for.
func validateShareContext(ctx context.Context, contextID *string) error {
	sc, ok := pkgauth.ShareContextFrom(ctx)
	if !ok || contextID == nil || *contextID == "" {
		return nil
	}
	if *contextID != sc.SessionID {
		return fmt.Errorf("share token is not valid for session %q", *contextID)
	}
	return nil
}

func injectInitiatedBy(ctx context.Context, msg *protocol.Message) {
	if _, ok := pkgauth.ShareContextFrom(ctx); !ok {
		return
	}
	session, ok := pkgauth.AuthSessionFrom(ctx)
	if !ok {
		return
	}
	userID := session.Principal().User.ID
	if userID == "" {
		return
	}
	if msg.Metadata == nil {
		msg.Metadata = make(map[string]any)
	}
	msg.Metadata["initiated_by"] = userID
}

func (m *PassthroughManager) OnSendMessage(ctx context.Context, request protocol.SendMessageParams) (*protocol.MessageResult, error) {
	if request.Message.MessageID == "" {
		request.Message.MessageID = protocol.GenerateMessageID()
	}
	if request.Message.Kind == "" {
		request.Message.Kind = protocol.KindMessage
	}
	if err := validateShareContext(ctx, request.Message.ContextID); err != nil {
		return nil, err
	}
	injectInitiatedBy(ctx, &request.Message)
	return m.client.SendMessage(ctx, request)
}

func (m *PassthroughManager) OnSendMessageStream(ctx context.Context, request protocol.SendMessageParams) (<-chan protocol.StreamingMessageEvent, error) {
	if request.Message.MessageID == "" {
		request.Message.MessageID = protocol.GenerateMessageID()
	}
	if request.Message.Kind == "" {
		request.Message.Kind = protocol.KindMessage
	}
	if err := validateShareContext(ctx, request.Message.ContextID); err != nil {
		return nil, err
	}
	injectInitiatedBy(ctx, &request.Message)
	return m.client.StreamMessage(ctx, request)
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
