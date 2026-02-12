package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go-adk/pkg/adk"
	"github.com/kagent-dev/kagent/go-adk/pkg/adk/models"
	"github.com/kagent-dev/kagent/go-adk/pkg/core"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
	"trpc.group/trpc-go/trpc-a2a-go/server"
	"trpc.group/trpc-go/trpc-a2a-go/taskmanager"
)

// ConfigurableRunner uses agent configuration to run the agent
// Now uses Google ADK Runner for agent execution (matching Python implementation)
type ConfigurableRunner struct {
	config          *core.AgentConfig
	skillsDirectory string
	skillsTool      *core.SkillsTool
	bashTool        *core.BashTool
	fileTools       *core.FileTools
	googleADKRunner *adk.GoogleADKRunnerWrapper // Wrapper around Google ADK Runner
	logger          logr.Logger
}

func NewConfigurableRunner(config *core.AgentConfig, skillsDirectory string, logger logr.Logger) *ConfigurableRunner {
	runner := &ConfigurableRunner{
		config:          config,
		skillsDirectory: skillsDirectory,
		logger:          logger,
	}

	// Initialize skills tools if skills directory exists
	if skillsDirectory != "" {
		if _, err := os.Stat(skillsDirectory); err == nil {
			runner.skillsTool = core.NewSkillsTool(skillsDirectory)
			runner.bashTool = core.NewBashTool(skillsDirectory)
			runner.fileTools = &core.FileTools{}
		}
	}

	return runner
}

func (r *ConfigurableRunner) Run(ctx context.Context, args map[string]interface{}) (<-chan interface{}, error) {
	sessionID, userID := extractSessionAndUserFromArgs(args)
	message := extractMessageFromArgs(args, r.logger)

	if r.skillsDirectory != "" && sessionID != "" {
		if _, err := core.InitializeSessionPath(sessionID, r.skillsDirectory); err != nil {
			return nil, fmt.Errorf("failed to initialize session path: %w", err)
		}
	}

	if r.config == nil || r.config.Model == nil {
		return fallbackChannel(r.config), nil
	}

	if message == nil {
		if r.logger.GetSink() != nil {
			r.logger.Info("Skipping LLM execution: message is nil", "configExists", r.config != nil, "modelExists", r.config.Model != nil)
		}
		return fallbackChannelNoMessage(r.config), nil
	}

	sessionService, _ := args[adk.ArgKeySessionService].(core.SessionService)
	appName, _ := args[adk.ArgKeyAppName].(string)
	if r.googleADKRunner == nil {
		if r.logger.GetSink() != nil {
			r.logger.Info("Creating Google ADK Runner", "modelType", r.config.Model.GetType(), "sessionID", sessionID, "userID", userID, "appName", appName)
		}
		adkRunner, err := adk.CreateGoogleADKRunner(r.config, sessionService, appName, r.logger)
		if err != nil {
			if r.logger.GetSink() != nil {
				r.logger.Error(err, "Failed to create Google ADK Runner")
			}
			return fallbackErrorChannel(err), nil
		}
		r.googleADKRunner = adk.NewGoogleADKRunnerWrapper(adkRunner, r.logger)
	}

	if r.logger.GetSink() != nil {
		r.logger.Info("Executing agent with Google ADK Runner", "messageID", message.MessageID, "partsCount", len(message.Parts))
	}
	return r.googleADKRunner.Run(ctx, args)
}

// extractSessionAndUserFromArgs returns session_id and user_id from args.
func extractSessionAndUserFromArgs(args map[string]interface{}) (sessionID, userID string) {
	if sid, ok := args[adk.ArgKeySessionID].(string); ok {
		sessionID = sid
	}
	if uid, ok := args[adk.ArgKeyUserID].(string); ok {
		userID = uid
	}
	return sessionID, userID
}

// extractMessageFromArgs extracts *protocol.Message from args[ArgKeyMessage] or args[ArgKeyNewMessage].
func extractMessageFromArgs(args map[string]interface{}, logger logr.Logger) *protocol.Message {
	if msg := tryMessage(args[adk.ArgKeyMessage], adk.ArgKeyMessage, logger); msg != nil {
		return msg
	}
	if msg := tryMessage(args[adk.ArgKeyNewMessage], adk.ArgKeyNewMessage, logger); msg != nil {
		return msg
	}
	if logger.GetSink() != nil {
		logger.Info("No message found in args", "argsKeys", getMapKeys(args))
		for _, key := range []string{adk.ArgKeyMessage, adk.ArgKeyNewMessage} {
			if v, ok := args[key]; ok {
				logger.Info("args key exists but wrong type", "key", key, "type", fmt.Sprintf("%T", v), "value", fmt.Sprintf("%+v", v))
			}
		}
	}
	return nil
}

func tryMessage(val interface{}, key string, logger logr.Logger) *protocol.Message {
	if val == nil {
		return nil
	}
	if msg, ok := val.(*protocol.Message); ok {
		if logger.GetSink() != nil {
			logger.Info("Found message in args["+key+"]", "messageID", msg.MessageID, "role", msg.Role, "partsCount", len(msg.Parts))
		}
		return msg
	}
	if msg, ok := val.(protocol.Message); ok {
		if logger.GetSink() != nil {
			logger.Info("Found message in args["+key+"] (non-pointer)", "messageID", msg.MessageID, "role", msg.Role, "partsCount", len(msg.Parts))
		}
		return &msg
	}
	return nil
}

func fallbackChannel(config *core.AgentConfig) <-chan interface{} {
	ch := make(chan interface{}, 1)
	go func() {
		defer close(ch)
		if config != nil && config.Model != nil {
			ch <- fmt.Sprintf("Using model: %s with instruction: %s", config.Model.GetType(), config.Instruction)
		} else {
			ch <- "Hello from Go ADK!"
		}
	}()
	return ch
}

func fallbackChannelNoMessage(config *core.AgentConfig) <-chan interface{} {
	ch := make(chan interface{}, 1)
	go func() {
		defer close(ch)
		ch <- fmt.Sprintf("Using model: %s with instruction: %s (no message provided)", config.Model.GetType(), config.Instruction)
	}()
	return ch
}

func fallbackErrorChannel(err error) <-chan interface{} {
	ch := make(chan interface{}, 1)
	go func() {
		defer close(ch)
		ch <- fmt.Sprintf("Error creating Google ADK Runner: %v", err)
	}()
	return ch
}

// getMapKeys returns keys from a map for logging
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// resolveContextID returns the session/context ID from the message for event persistence.
// Prefer message.ContextID (A2A contextId), then message.Metadata[kagent_session_id], then
// metadata contextId/context_id. Returns nil if none set so caller can generate one.
func resolveContextID(msg *protocol.Message) *string {
	if msg == nil {
		return nil
	}
	if msg.ContextID != nil && *msg.ContextID != "" {
		return msg.ContextID
	}
	if msg.Metadata != nil {
		for _, key := range []string{core.GetKAgentMetadataKey(core.MetadataKeySessionID), "contextId", "context_id"} {
			if v, ok := msg.Metadata[key]; ok {
				if s, ok := v.(string); ok && s != "" {
					return &s
				}
			}
		}
	}
	return nil
}

// ADKTaskManager implements taskmanager.TaskManager using the A2aAgentExecutor
type ADKTaskManager struct {
	executor              *core.A2aAgentExecutor
	taskStore             *core.KAgentTaskStore
	pushNotificationStore *core.KAgentPushNotificationStore
	logger                logr.Logger
}

func NewADKTaskManager(executor *core.A2aAgentExecutor, taskStore *core.KAgentTaskStore, pushNotificationStore *core.KAgentPushNotificationStore, logger logr.Logger) taskmanager.TaskManager {
	return &ADKTaskManager{
		executor:              executor,
		taskStore:             taskStore,
		pushNotificationStore: pushNotificationStore,
		logger:                logger,
	}
}

func (m *ADKTaskManager) OnSendMessage(ctx context.Context, request protocol.SendMessageParams) (*protocol.MessageResult, error) {
	// Extract context_id from request (session_id for history/DB); prefer message.contextId then metadata
	contextID := resolveContextID(&request.Message)
	if contextID == nil || *contextID == "" {
		contextIDString := uuid.New().String()
		contextID = &contextIDString
	}

	// Generate task ID
	taskID := uuid.New().String()
	if request.Message.TaskID != nil && *request.Message.TaskID != "" {
		taskID = *request.Message.TaskID
	}

	// Create an in-memory event queue
	innerQueue := &InMemoryEventQueue{events: []protocol.Event{}}
	// Wrap with task-saving queue (matching Python: event_queue automatically saves tasks)
	queue := NewTaskSavingEventQueue(innerQueue, m.taskStore, taskID, *contextID, m.logger)

	err := m.executor.Execute(ctx, &request, queue, taskID, *contextID)
	if err != nil {
		return nil, err
	}

	// Extract the final message from events
	var finalMessage *protocol.Message
	for _, event := range innerQueue.events {
		if statusEvent, ok := event.(*protocol.TaskStatusUpdateEvent); ok && statusEvent.Final {
			if statusEvent.Status.Message != nil {
				finalMessage = statusEvent.Status.Message
			}
		}
	}

	return &protocol.MessageResult{
		Result: finalMessage,
	}, nil
}

func (m *ADKTaskManager) OnSendMessageStream(ctx context.Context, request protocol.SendMessageParams) (<-chan protocol.StreamingMessageEvent, error) {
	ch := make(chan protocol.StreamingMessageEvent)
	innerQueue := &StreamingEventQueue{ch: ch}

	// Extract context_id from request (used as session_id for history/DB). Prefer message.contextId,
	// then message.metadata.kagent_session_id, then generate. Using client session ID ensures events
	// are stored to the same session the UI created.
	contextID := resolveContextID(&request.Message)
	if contextID == nil || *contextID == "" {
		contextIDString := uuid.New().String()
		contextID = &contextIDString
		if m.logger.GetSink() != nil {
			m.logger.Info("No context_id in request; generated new one — events may not match UI session",
				"generatedContextID", *contextID)
		}
	}

	// Generate task ID
	taskID := uuid.New().String()
	if request.Message.TaskID != nil && *request.Message.TaskID != "" {
		taskID = *request.Message.TaskID
	}

	// Wrap with task-saving queue (matching Python: event_queue automatically saves tasks)
	queue := NewTaskSavingEventQueue(innerQueue, m.taskStore, taskID, *contextID, m.logger)

	go func() {
		defer close(ch)
		err := m.executor.Execute(ctx, &request, queue, taskID, *contextID)
		if err != nil {
			ch <- protocol.StreamingMessageEvent{
				Result: &protocol.TaskStatusUpdateEvent{
					Kind:      "status-update",
					TaskID:    taskID,
					ContextID: *contextID,
					Status: protocol.TaskStatus{
						State: protocol.TaskStateFailed,
						Message: &protocol.Message{
							MessageID: uuid.New().String(),
							Role:      protocol.MessageRoleAgent,
							Parts: []protocol.Part{
								protocol.NewTextPart(err.Error()),
							},
						},
						Timestamp: time.Now().UTC().Format(time.RFC3339),
					},
					Final: true,
				},
			}
		}
	}()

	return ch, nil
}

func (m *ADKTaskManager) OnGetTask(ctx context.Context, params protocol.TaskQueryParams) (*protocol.Task, error) {
	// If no task store is available, return error (matching Python behavior when task_store is None)
	if m.taskStore == nil {
		return nil, fmt.Errorf("task store not available")
	}

	// Extract task ID from params
	// TaskQueryParams should have a TaskID field
	taskID := params.ID
	if taskID == "" {
		return nil, fmt.Errorf("task ID is required")
	}

	// Use TaskStore.Get to retrieve the task (matching Python KAgentTaskStore.get)
	task, err := m.taskStore.Get(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	// Return nil if task not found (matching Python behavior)
	if task == nil {
		return nil, nil
	}

	return task, nil
}

func (m *ADKTaskManager) OnCancelTask(ctx context.Context, params protocol.TaskIDParams) (*protocol.Task, error) {
	// If no task store is available, return error (matching Python behavior when task_store is None)
	if m.taskStore == nil {
		return nil, fmt.Errorf("task store not available")
	}

	// Extract task ID from params
	taskID := params.ID
	if taskID == "" {
		return nil, fmt.Errorf("task ID is required")
	}

	// First, get the task to return it
	task, err := m.taskStore.Get(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	// If task not found, return nil (matching Python behavior)
	if task == nil {
		return nil, nil
	}

	// Delete the task using TaskStore.Delete (matching Python KAgentTaskStore.delete)
	if err := m.taskStore.Delete(ctx, taskID); err != nil {
		return nil, fmt.Errorf("failed to delete task: %w", err)
	}

	// Return the deleted task (matching A2A protocol behavior)
	return task, nil
}

func (m *ADKTaskManager) OnPushNotificationSet(ctx context.Context, params protocol.TaskPushNotificationConfig) (*protocol.TaskPushNotificationConfig, error) {
	// If no push notification store is available, return error
	if m.pushNotificationStore == nil {
		return nil, fmt.Errorf("push notification store not available")
	}

	// Use PushNotificationStore.Set to store the configuration
	config, err := m.pushNotificationStore.Set(ctx, &params)
	if err != nil {
		return nil, fmt.Errorf("failed to set push notification: %w", err)
	}

	return config, nil
}

func (m *ADKTaskManager) OnPushNotificationGet(ctx context.Context, params protocol.TaskIDParams) (*protocol.TaskPushNotificationConfig, error) {
	// If no push notification store is available, return error
	if m.pushNotificationStore == nil {
		return nil, fmt.Errorf("push notification store not available")
	}

	// Extract task ID from params
	taskID := params.ID
	if taskID == "" {
		return nil, fmt.Errorf("task ID is required")
	}

	// Note: TaskIDParams might need to include ConfigID, but for now we'll need to handle it
	// The A2A protocol might pass config ID differently - this may need adjustment
	// For now, returning error if we can't determine the config ID
	// In practice, the A2A protocol should provide the config ID in the params
	return nil, fmt.Errorf("config ID extraction from TaskIDParams not yet implemented - may need protocol update")
}

func (m *ADKTaskManager) OnResubscribe(ctx context.Context, params protocol.TaskIDParams) (<-chan protocol.StreamingMessageEvent, error) {
	// Extract task ID from params
	taskID := params.ID
	if taskID == "" {
		return nil, fmt.Errorf("task ID is required")
	}

	// If no task store is available, return error
	if m.taskStore == nil {
		return nil, fmt.Errorf("task store not available")
	}

	// Get the task to retrieve its context and history
	task, err := m.taskStore.Get(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	if task == nil {
		return nil, fmt.Errorf("task not found: %s", taskID)
	}

	// Extract context ID from task
	contextID := task.ContextID
	if contextID == "" {
		return nil, fmt.Errorf("task has no context ID")
	}

	// Create streaming channel
	ch := make(chan protocol.StreamingMessageEvent)

	go func() {
		defer close(ch)

		// Replay task history as streaming events (matching A2A resubscribe behavior)
		// History contains messages that were already sent, so we replay them
		if task.History != nil {
			for i := range task.History {
				// Convert message to streaming event
				// Use index to avoid address-of-loop-variable bug
				select {
				case ch <- protocol.StreamingMessageEvent{
					Result: &task.History[i],
				}:
				case <-ctx.Done():
					return
				}
			}
		}

		// Send current task status as a status update event
		// Determine if task is final based on state
		isFinal := task.Status.State == protocol.TaskStateCompleted ||
			task.Status.State == protocol.TaskStateFailed

		select {
		case ch <- protocol.StreamingMessageEvent{
			Result: &protocol.TaskStatusUpdateEvent{
				Kind:      "status-update",
				TaskID:    taskID,
				ContextID: contextID,
				Status:    task.Status,
				Final:     isFinal,
			},
		}:
		case <-ctx.Done():
			return
		}

		// If task is still active (not completed/failed/cancelled), resubscription is complete.
		// In a full implementation with active task tracking we would continue streaming new events.
	}()

	return ch, nil
}

// TaskSavingEventQueue wraps an EventQueue and automatically saves tasks to task store
// after each event is enqueued (matching Python A2A SDK behavior).
// contextID (session ID) is set on the task so GET /api/sessions/:id/tasks returns it (backend uses task.ContextID as session_id).
// It keeps the task in memory so each save uses accumulated state and never overwrites with stale data (e.g. artifact result).
type TaskSavingEventQueue struct {
	inner       core.EventQueue
	taskStore   *core.KAgentTaskStore
	taskID      string
	contextID   string // session ID so tasks show in UI session tasks list
	logger      logr.Logger
	currentTask *protocol.Task // in-memory task so we don't overwrite with stale Get (ensures artifact/result is kept)
}

func NewTaskSavingEventQueue(inner core.EventQueue, taskStore *core.KAgentTaskStore, taskID, contextID string, logger logr.Logger) *TaskSavingEventQueue {
	return &TaskSavingEventQueue{
		inner:     inner,
		taskStore: taskStore,
		taskID:    taskID,
		contextID: contextID,
		logger:    logger,
	}
}

func (q *TaskSavingEventQueue) EnqueueEvent(ctx context.Context, event protocol.Event) error {
	if err := q.inner.EnqueueEvent(ctx, event); err != nil {
		return err
	}
	if q.taskStore == nil {
		return nil
	}
	task := q.loadOrCreateTask(ctx)
	task.ContextID = q.contextID
	applyEventToTask(task, event)
	if err := q.taskStore.Save(ctx, task); err != nil {
		if q.logger.GetSink() != nil {
			q.logger.Error(err, "Failed to save task after enqueueing event", "taskID", q.taskID, "eventType", fmt.Sprintf("%T", event))
		}
	} else if q.logger.GetSink() != nil {
		q.logger.V(1).Info("Saved task after enqueueing event", "taskID", q.taskID, "eventType", fmt.Sprintf("%T", event))
	}
	return nil
}

func (q *TaskSavingEventQueue) loadOrCreateTask(ctx context.Context) *protocol.Task {
	if q.currentTask != nil {
		return q.currentTask
	}
	loaded, err := q.taskStore.Get(ctx, q.taskID)
	if err != nil || loaded == nil {
		q.currentTask = &protocol.Task{ID: q.taskID, ContextID: q.contextID}
	} else {
		q.currentTask = loaded
	}
	return q.currentTask
}

func applyEventToTask(task *protocol.Task, event protocol.Event) {
	if statusEvent, ok := event.(*protocol.TaskStatusUpdateEvent); ok {
		task.Status = statusEvent.Status
		if statusEvent.Status.Message != nil {
			if task.History == nil {
				task.History = []protocol.Message{}
			}
			task.History = append(task.History, *statusEvent.Status.Message)
		}
		return
	}
	if artifactEvent, ok := event.(*protocol.TaskArtifactUpdateEvent); ok && len(artifactEvent.Artifact.Parts) > 0 {
		if task.History == nil {
			task.History = []protocol.Message{}
		}
		task.History = append(task.History, protocol.Message{
			Kind:      protocol.KindMessage,
			MessageID: uuid.New().String(),
			Role:      protocol.MessageRoleAgent,
			Parts:     artifactEvent.Artifact.Parts,
		})
	}
}

// InMemoryEventQueue stores events in memory
type InMemoryEventQueue struct {
	events []protocol.Event
}

func (q *InMemoryEventQueue) EnqueueEvent(ctx context.Context, event protocol.Event) error {
	q.events = append(q.events, event)
	return nil
}

// StreamingEventQueue streams events to a channel
type StreamingEventQueue struct {
	ch chan protocol.StreamingMessageEvent
}

func (q *StreamingEventQueue) EnqueueEvent(ctx context.Context, event protocol.Event) error {
	var streamEvent protocol.StreamingMessageEvent
	if statusEvent, ok := event.(*protocol.TaskStatusUpdateEvent); ok {
		streamEvent = protocol.StreamingMessageEvent{
			Result: statusEvent,
		}
	} else if artifactEvent, ok := event.(*protocol.TaskArtifactUpdateEvent); ok {
		streamEvent = protocol.StreamingMessageEvent{
			Result: artifactEvent,
		}
	} else {
		// For unknown event types, try to convert to Message if possible
		// Otherwise, we can't create a valid StreamingMessageEvent
		// This should not happen in normal operation
		return fmt.Errorf("unsupported event type: %T", event)
	}

	select {
	case q.ch <- streamEvent:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// buildAppName builds the app_name from KAGENT_NAMESPACE and KAGENT_NAME environment variables.
// Format: {namespace}__NS__{name} where dashes are replaced with underscores.
// This matches Python KAgentConfig.app_name = self.namespace + "__NS__" + self.name
// Falls back to agentCard.Name if environment variables are not set, or "go-adk-agent" as default.
func buildAppName(agentCard *server.AgentCard, logger logr.Logger) string {
	kagentName := os.Getenv("KAGENT_NAME")
	kagentNamespace := os.Getenv("KAGENT_NAMESPACE")

	// If both are set, use the Python format: namespace__NS__name
	if kagentNamespace != "" && kagentName != "" {
		// Replace dashes with underscores (matching Python: self._name.replace("-", "_"))
		namespace := strings.ReplaceAll(kagentNamespace, "-", "_")
		name := strings.ReplaceAll(kagentName, "-", "_")
		appName := namespace + "__NS__" + name
		logger.Info("Built app_name from environment variables",
			"KAGENT_NAMESPACE", kagentNamespace,
			"KAGENT_NAME", kagentName,
			"app_name", appName)
		return appName
	}

	// Fallback to agent card name if available
	if agentCard != nil && agentCard.Name != "" {
		logger.Info("Using agent card name as app_name (KAGENT_NAMESPACE/KAGENT_NAME not set)",
			"app_name", agentCard.Name)
		return agentCard.Name
	}

	// Default fallback
	logger.Info("Using default app_name (KAGENT_NAMESPACE/KAGENT_NAME not set and no agent card)",
		"app_name", "go-adk-agent")
	return "go-adk-agent"
}

// setupLogger initializes and returns a logr.Logger with the specified log level.
// The log level string is case-insensitive and supports: debug, info, warn/warning, error.
// Defaults to info level if an invalid level is provided.
// Returns both the logr.Logger and the underlying zap.Logger (for cleanup).
func setupLogger(logLevel string) (logr.Logger, *zap.Logger) {
	// Parse log level and set zap level
	var zapLevel zapcore.Level
	switch strings.ToLower(logLevel) {
	case "debug":
		zapLevel = zapcore.DebugLevel
	case "info":
		zapLevel = zapcore.InfoLevel
	case "warn", "warning":
		zapLevel = zapcore.WarnLevel
	case "error":
		zapLevel = zapcore.ErrorLevel
	default:
		zapLevel = zapcore.InfoLevel
	}

	// Configure zap logger with the specified level
	config := zap.NewProductionConfig()
	config.Level = zap.NewAtomicLevelAt(zapLevel)
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	zapLogger, err := config.Build()
	if err != nil {
		// Fallback to development logger if production config fails
		devConfig := zap.NewDevelopmentConfig()
		devConfig.Level = zap.NewAtomicLevelAt(zapLevel)
		zapLogger, _ = devConfig.Build()
	}
	logger := zapr.NewLogger(zapLogger)

	logger.Info("Logger initialized", "level", logLevel)
	return logger, zapLogger
}

func main() {
	// Parse command line flags
	logLevel := flag.String("log-level", "info", "Set the logging level (debug, info, warn, error)")
	host := flag.String("host", "", "Set the host address to bind to (default: empty, binds to all interfaces)")
	portFlag := flag.String("port", "", "Set the port to listen on (overrides PORT environment variable)")
	filepathFlag := flag.String("filepath", "", "Set the config directory path (overrides CONFIG_DIR environment variable)")
	flag.Parse()

	logger, zapLogger := setupLogger(*logLevel)
	defer func() {
		_ = zapLogger.Sync()
	}()

	// Get port from flag, environment variable, or default
	port := *portFlag
	if port == "" {
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = "8080"
	}

	// Get config directory from flag, environment variable, or default
	configDir := *filepathFlag
	if configDir == "" {
		configDir = os.Getenv("CONFIG_DIR")
	}
	if configDir == "" {
		configDir = "/config"
	}

	kagentURL := os.Getenv("KAGENT_URL")
	if kagentURL == "" {
		kagentURL = "http://localhost:8083"
	}

	// Load agent configuration from config directory (matching Python implementation)
	agentConfig, agentCard, err := adk.LoadAgentConfigs(configDir)
	if err != nil {
		logger.Info("Failed to load agent config, using default configuration", "configDir", configDir, "error", err)
		// Create default config if loading fails
		streamDefault := false
		executeCodeDefault := false
		agentConfig = &core.AgentConfig{
			Stream:      &streamDefault,
			ExecuteCode: &executeCodeDefault,
		}
		agentCard = &server.AgentCard{
			Name:        "go-adk-agent",
			Description: "Go-based Agent Development Kit",
		}
	} else {
		logger.Info("Loaded agent config", "configDir", configDir)
		logger.Info("AgentConfig summary", "summary", adk.GetAgentConfigSummary(agentConfig))
		logger.Info("Agent configuration",
			"model", agentConfig.Model.GetType(),
			"stream", agentConfig.GetStream(),
			"executeCode", agentConfig.GetExecuteCode(),
			"httpTools", len(agentConfig.HttpTools),
			"sseTools", len(agentConfig.SseTools),
			"remoteAgents", len(agentConfig.RemoteAgents))
	}

	// Build app_name from KAGENT_NAMESPACE and KAGENT_NAME (matching Python KAgentConfig.app_name)
	appName := buildAppName(agentCard, logger)
	logger.Info("Final app_name for session creation", "app_name", appName)

	// Create token service for k8s token management (matching Python implementation)
	var tokenService *core.KAgentTokenService
	if kagentURL != "" {
		tokenService = core.NewKAgentTokenService(appName)
		ctx := context.Background()
		if err := tokenService.Start(ctx); err != nil {
			logger.Error(err, "Failed to start token service")
		} else {
			logger.Info("Token service started")
		}
		defer tokenService.Stop()
	}

	// Create session service (use nil for in-memory if KAGENT_URL is not set)
	var sessionService core.SessionService
	if kagentURL != "" {
		// Use token service for authenticated requests
		var httpClient *http.Client
		if tokenService != nil {
			httpClient = core.NewHTTPClientWithToken(tokenService)
		} else {
			httpClient = &http.Client{Timeout: 30 * time.Second}
		}
		sessionService = core.NewKAgentSessionServiceWithLogger(kagentURL, httpClient, logger)
		logger.Info("Using KAgent session service", "url", kagentURL)
	} else {
		logger.Info("No KAGENT_URL set, using in-memory session (sessions will not persist)")
	}

	// Create task store for persisting tasks to KAgent
	var taskStore *core.KAgentTaskStore
	var pushNotificationStore *core.KAgentPushNotificationStore
	if kagentURL != "" {
		// Use token service for authenticated requests
		var httpClient *http.Client
		if tokenService != nil {
			httpClient = core.NewHTTPClientWithToken(tokenService)
		} else {
			httpClient = &http.Client{Timeout: 30 * time.Second}
		}
		taskStore = core.NewKAgentTaskStoreWithClient(kagentURL, httpClient)
		pushNotificationStore = core.NewKAgentPushNotificationStoreWithClient(kagentURL, httpClient)
		logger.Info("Using KAgent task store", "url", kagentURL)
		logger.Info("Using KAgent push notification store", "url", kagentURL)
	} else {
		logger.Info("No KAGENT_URL set, task persistence and push notifications disabled")
	}

	// Check for skills directory (matching Python's KAGENT_SKILLS_FOLDER)
	skillsDirectory := os.Getenv("KAGENT_SKILLS_FOLDER")
	if skillsDirectory != "" {
		logger.Info("Skills directory configured", "directory", skillsDirectory)
	} else {
		// Default to /skills if not set
		skillsDirectory = "/skills"
		logger.Info("Using default skills directory", "directory", skillsDirectory)
	}

	// Create runner with agent config and skills
	runner := NewConfigurableRunner(agentConfig, skillsDirectory, logger)

	// Use stream setting from agent config (matches Python: agent_config.stream if agent_config and agent_config.stream is not None else False)
	stream := false // Default: no streaming
	if agentConfig != nil {
		stream = agentConfig.GetStream()
	}

	executor := core.NewA2aAgentExecutorWithLogger(runner, adk.NewEventConverter(), core.A2aAgentExecutorConfig{
		Stream:           stream,
		ExecutionTimeout: models.DefaultExecutionTimeout,
	}, sessionService, taskStore, appName, logger)

	taskManager := NewADKTaskManager(executor, taskStore, pushNotificationStore, logger)

	// Use loaded agent card or create default
	if agentCard == nil {
		agentCard = &server.AgentCard{
			Name:        "go-adk-agent",
			Description: "Go-based Agent Development Kit",
			Version:     "0.1.0",
		}
	}

	// Initialize A2A server with agent card
	a2aServer, err := server.NewA2AServer(*agentCard, taskManager)
	if err != nil {
		logger.Error(err, "Failed to create A2A server")
		os.Exit(1)
	}

	// Create mux to handle both A2A routes and health endpoint
	mux := http.NewServeMux()

	// Health endpoint for Kubernetes readiness probe
	// Returns 200 OK when the service is ready to accept traffic
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Healthz endpoint (alternative common path for Kubernetes)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// All other routes go to A2A server
	// Note: Health endpoints must be registered before the catch-all "/" route
	mux.Handle("/", a2aServer.Handler())

	// Create HTTP server
	addr := ":" + port
	if *host != "" {
		addr = *host + ":" + port
	}
	httpServer := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	logger.Info("Starting Go ADK server", "addr", addr, "host", *host, "port", port)

	// Graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(err, "Server failed")
			os.Exit(1)
		}
	}()

	<-stop
	logger.Info("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error(err, "Error shutting down server")
	}
}
