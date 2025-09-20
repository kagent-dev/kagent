package a2a

import (
	"context"
	"errors"

	"github.com/kagent-dev/kagent/go/internal/database"
	"github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/auth"
	"gorm.io/gorm"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
	"trpc.group/trpc-go/trpc-a2a-go/taskmanager"
)

// RecordingManager is similar to the PassthroughManager, but it also stores the task and session information in the database.
// It is used for remote agents, which do not have a connection to kagent, so we use this middleware to handle that.
// Note: We expect the remote agent to handle its task history + token information which we store and parse from.
type RecordingManager struct {
	client   *client.A2AClient
	dbClient database.Client
	agentRef string
}

func NewRecordingManager(client *client.A2AClient, dbClient database.Client, agentRef string) taskmanager.TaskManager {
	return &RecordingManager{
		client:   client,
		dbClient: dbClient,
		agentRef: agentRef,
	}
}

// getUserID extracts user ID from context.
// similar to _get_user_id in kagent-adk's request_converter.py
func (m *RecordingManager) getUserID(ctx context.Context, contextID string) string {
	if session, ok := auth.AuthSessionFrom(ctx); ok {
		if session.Principal().User.ID != "" {
			return session.Principal().User.ID
		}
	}

	return "A2A_USER_" + contextID
}

func (m *RecordingManager) OnSendMessage(ctx context.Context, request protocol.SendMessageParams) (*protocol.MessageResult, error) {
	if request.Message.MessageID == "" {
		request.Message.MessageID = protocol.GenerateMessageID()
	}
	if request.Message.Kind == "" {
		request.Message.Kind = protocol.KindMessage
	}

	result, err := m.client.SendMessage(ctx, request)
	if err != nil {
		return nil, err
	}

	if task, ok := result.Result.(*protocol.Task); ok {
		logger := ctrllog.FromContext(ctx).WithName("recording-manager")

		userID := m.getUserID(ctx, task.ContextID)

		// Get agent ID from the agent ref
		var agentID *string
		if m.agentRef != "" {
			agentIDStr := utils.ConvertToPythonIdentifier(m.agentRef)
			agentID = &agentIDStr
		}

		// Check if session already exists
		session, err := m.dbClient.GetSession(task.ContextID, userID)
		if err != nil {
			if err != gorm.ErrRecordNotFound {
				logger.Error(err, "Failed to get session", "contextID", task.ContextID)
				return nil, err
			}

			// Session doesn't exist, create a new one
			session = &database.Session{
				ID:      task.ContextID,
				UserID:  userID,
				AgentID: agentID,
			}

			if err := m.dbClient.StoreSession(session); err != nil {
				logger.Error(err, "Failed to create session", "contextID", task.ContextID, "sessionID", session.ID, "agentID", session.AgentID)
				return nil, err
			}
		}

		// Store Task
		if err := m.dbClient.StoreTask(task); err != nil {
			logger.Error(err, "Failed to store task", "taskID", task.ID, "contextID", task.ContextID)
			return nil, err
		}
	}

	return result, nil
}

func (m *RecordingManager) OnSendMessageStream(ctx context.Context, request protocol.SendMessageParams) (<-chan protocol.StreamingMessageEvent, error) {
	if request.Message.MessageID == "" {
		request.Message.MessageID = protocol.GenerateMessageID()
	}
	if request.Message.Kind == "" {
		request.Message.Kind = protocol.KindMessage
	}

	if request.Message.ContextID == nil || *request.Message.ContextID == "" {
		return nil, errors.New("contextID is required")
	}

	if err := m.ensureSession(ctx, *request.Message.ContextID); err != nil {
		return nil, err
	}

	upstream, err := m.client.StreamMessage(ctx, request)
	if err != nil {
		return nil, err
	}

	out := make(chan protocol.StreamingMessageEvent)

	// TODO: We shouldn't _expect_ a Task to be sent during streaming.
	// If not, this means we'd need to have an internal Task. This way we can store it in the database for future reference.
	go func() {
		defer close(out)
		logger := ctrllog.FromContext(ctx).WithName("recording-manager")

		for ev := range upstream {
			// Forward to client
			out <- ev

			switch v := ev.Result.(type) {
			case *protocol.Message:
				if v != nil {
					logger.V(1).Info("Remote agent message",
						"messageID", v.MessageID,
						"contextID", v.ContextID,
					)
				}
			case *protocol.TaskArtifactUpdateEvent:
				if v != nil {
					logger.V(1).Info("Remote agent artifact update",
						"taskID", v.TaskID,
						"contextID", v.ContextID,
						"artifactParts", len(v.Artifact.Parts),
						"metadata", v.Artifact.Metadata)

					// Ensure a task exists for this artifact update
					m.ensureTaskExists(ctx, v.TaskID, v.ContextID)
				}
			case *protocol.TaskStatusUpdateEvent:
				if v != nil {
					logger.V(1).Info("Remote agent status update",
						"taskID", v.TaskID,
						"contextID", v.ContextID,
						"state", v.Status.State,
						"final", v.Final,
						"metadata", v.Metadata)
					// First ensure a task record exists, then update existing task
					m.ensureTaskExists(ctx, v.TaskID, v.ContextID)
					m.updateExistingTaskFromStatusEvent(ctx, v)
				}
			case *protocol.Task:
				if v != nil {
					logger.V(1).Info("Remote agent task",
						"taskID", v.ID,
						"contextID", v.ContextID,
						"status", v.Status,
						"metadata", v.Metadata)
				}

				if v != nil {
					if err := m.ensureSession(ctx, v.ContextID); err != nil {
						logger.Error(err, "Failed to ensure session for task", "contextID", v.ContextID)
						continue
					}

					// Store task
					if err := m.dbClient.StoreTask(v); err != nil {
						logger.Error(err, "Failed to store streaming task", "taskID", v.ID, "contextID", v.ContextID)
					}
				}
			default:
				logger.V(1).Info("UNHANDLED Received agent event", "event", v)
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

// ensureSession makes sure a session exists for the given contextID.
// It creates the session if it does not already exist.
func (m *RecordingManager) ensureSession(ctx context.Context, contextID string) error {
	logger := ctrllog.FromContext(ctx).WithName("recording-manager")

	userID := m.getUserID(ctx, contextID)
	var agentID *string
	if m.agentRef != "" {
		agentIDStr := utils.ConvertToPythonIdentifier(m.agentRef)
		agentID = &agentIDStr
	}

	if _, err := m.dbClient.GetSession(contextID, userID); err != nil {
		if err != gorm.ErrRecordNotFound {
			logger.Error(err, "Failed to get session", "contextID", contextID)
			return err
		}
		session := &database.Session{ID: contextID, UserID: userID, AgentID: agentID}
		if err := m.dbClient.StoreSession(session); err != nil {
			logger.Error(err, "Failed to create session", "contextID", contextID)
			return err
		}
	}
	return nil
}

// ensureTaskExists creates a task record if one does not already exist.
func (m *RecordingManager) ensureTaskExists(ctx context.Context, taskID string, contextID string) {
	if taskID == "" {
		return
	}
	logger := ctrllog.FromContext(ctx).WithName("recording-manager")
	// Ensure session exists as a prerequisite for storing a task
	if err := m.ensureSession(ctx, contextID); err != nil {
		logger.Error(err, "Failed to ensure session for task creation", "contextID", contextID)
		return
	}
	if _, err := m.dbClient.GetTask(taskID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			task := &protocol.Task{ID: taskID, ContextID: contextID, Kind: protocol.KindTask}
			if err := m.dbClient.StoreTask(task); err != nil {
				logger.Error(err, "Failed to create task", "taskID", taskID, "contextID", contextID)
			}
		} else {
			logger.Error(err, "Failed to get task", "taskID", taskID)
		}
	}
}

// updateExistingTaskFromStatusEvent persists status and metadata ONLY if the task already exists.
func (m *RecordingManager) updateExistingTaskFromStatusEvent(ctx context.Context, ev *protocol.TaskStatusUpdateEvent) {
	if ev == nil || ev.TaskID == "" {
		return
	}
	logger := ctrllog.FromContext(ctx).WithName("recording-manager")

	// Ensure session exists for the context
	if err := m.ensureSession(ctx, ev.ContextID); err != nil {
		logger.Error(err, "Failed to ensure session for status update", "contextID", ev.ContextID)
		return
	}

	// Load existing task
	task, err := m.dbClient.GetTask(ev.TaskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Do not create here; creation is handled separately
			return
		}
		logger.Error(err, "Failed to get task for status update", "taskID", ev.TaskID)
		return
	}

	// Update existing task fields
	if task.Metadata == nil {
		task.Metadata = map[string]interface{}{}
	}
	for k, v := range ev.Metadata {
		task.Metadata[k] = v
	}
	task.Status = ev.Status

	if err := m.dbClient.StoreTask(task); err != nil {
		logger.Error(err, "Failed to persist task status update", "taskID", ev.TaskID)
	}
}
