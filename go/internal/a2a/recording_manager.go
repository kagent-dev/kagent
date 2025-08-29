package a2a

import (
	"context"
	"sync/atomic"

	"github.com/kagent-dev/kagent/go/internal/database"
	"trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
	"trpc.group/trpc-go/trpc-a2a-go/taskmanager"
)

type RecordingManager struct {
	client   *client.A2AClient
	dbClient database.Client
}

func NewRecordingManager(client *client.A2AClient, dbClient database.Client) taskmanager.TaskManager {
	return &RecordingManager{
		client:   client,
		dbClient: dbClient,
	}
}

func (m *RecordingManager) OnSendMessage(ctx context.Context, request protocol.SendMessageParams) (*protocol.MessageResult, error) {
	if request.Message.MessageID == "" {
		request.Message.MessageID = protocol.GenerateMessageID()
	}
	if request.Message.Kind == "" {
		request.Message.Kind = protocol.KindMessage
	}
	return m.client.SendMessage(ctx, request)
}

func (m *RecordingManager) OnSendMessageStream(ctx context.Context, request protocol.SendMessageParams) (<-chan protocol.StreamingMessageEvent, error) {
	if request.Message.MessageID == "" {
		request.Message.MessageID = protocol.GenerateMessageID()
	}
	if request.Message.Kind == "" {
		request.Message.Kind = protocol.KindMessage
	}

	upstream, err := m.client.StreamMessage(ctx, request)
	if err != nil {
		return nil, err
	}

	// TODO: If DB is not available, passthrough? or should we return an error?
	if m.dbClient == nil {
		return upstream, nil
	}

	out := make(chan protocol.StreamingMessageEvent)

	var (
		contextID     string
		taskID        string
		assistantText string
		persisted     int32
	)

	// TODO: Verify this is correct method to capture messages, and the expected messages are being captured. Based on minimal manual testing, this works.
	go func() {
		defer close(out)
		for ev := range upstream {
			// forward to client
			out <- ev

			// capture for persistence
			switch v := ev.Result.(type) {
			case *protocol.TaskArtifactUpdateEvent:
				if v != nil {
					if contextID == "" {
						contextID = v.ContextID
					}
					if taskID == "" {
						taskID = v.TaskID
					}
					for _, p := range v.Artifact.Parts {
						if tp, ok := p.(*protocol.TextPart); ok {
							assistantText += tp.Text
						}
					}
				}
			case *protocol.TaskStatusUpdateEvent:
				if v != nil {
					if contextID == "" {
						contextID = v.ContextID
					}
					if taskID == "" {
						taskID = v.TaskID
					}
					if v.Status.Message != nil {
						// ignore status message text for persistence; keep only artifact text
					}
					if v.Final && atomic.CompareAndSwapInt32(&persisted, 0, 1) {
						// Prefer request's context ID to align with UI sessions
						var desiredContextID string
						if request.Message.ContextID != nil && *request.Message.ContextID != "" {
							desiredContextID = *request.Message.ContextID
						} else {
							desiredContextID = contextID
						}

						// Build minimal history: user then assistant
						userMsg := request.Message
						if userMsg.Kind == "" {
							userMsg.Kind = protocol.KindMessage
						}
						if userMsg.Role == "" {
							userMsg.Role = protocol.MessageRoleUser
						}
						if userMsg.ContextID == nil {
							userMsg.ContextID = &desiredContextID
						}
						if userMsg.TaskID == nil {
							userMsg.TaskID = &taskID
						}

						finalText := assistantText
						if finalText == "" && v.Status.Message != nil {
							finalText = ExtractText(*v.Status.Message)
						}
						assistantMsg := protocol.Message{
							Kind:  protocol.KindMessage,
							Role:  protocol.MessageRoleAgent,
							Parts: []protocol.Part{protocol.NewTextPart(finalText)},
						}
						if assistantMsg.MessageID == "" {
							assistantMsg.MessageID = protocol.GenerateMessageID()
						}
						assistantMsg.ContextID = &desiredContextID
						assistantMsg.TaskID = &taskID

						task := &protocol.Task{
							ID:        taskID,
							ContextID: desiredContextID,
							History:   []protocol.Message{userMsg, assistantMsg},
						}
						_ = m.dbClient.StoreTask(task)
					}
				}
			}
		}
	}()

	return out, nil
}

func (m *RecordingManager) OnGetTask(ctx context.Context, params protocol.TaskQueryParams) (*protocol.Task, error) {
	return m.client.GetTasks(ctx, params)
}

func (m *RecordingManager) OnCancelTask(ctx context.Context, params protocol.TaskIDParams) (*protocol.Task, error) {
	return m.client.CancelTasks(ctx, params)
}

func (m *RecordingManager) OnPushNotificationSet(ctx context.Context, params protocol.TaskPushNotificationConfig) (*protocol.TaskPushNotificationConfig, error) {
	return m.client.SetPushNotification(ctx, params)
}

func (m *RecordingManager) OnPushNotificationGet(ctx context.Context, params protocol.TaskIDParams) (*protocol.TaskPushNotificationConfig, error) {
	return m.client.GetPushNotification(ctx, params)
}

func (m *RecordingManager) OnResubscribe(ctx context.Context, params protocol.TaskIDParams) (<-chan protocol.StreamingMessageEvent, error) {
	return m.client.ResubscribeTask(ctx, params)
}
