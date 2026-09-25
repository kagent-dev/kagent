package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"slices"
	"strings"
	"time"

	"log/slog"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/kagent-dev/kagent/go/adk/pkg/auth"
	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	"github.com/kagent-dev/kagent/go/adk/pkg/telemetry"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiadk "github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	"go.opentelemetry.io/otel/attribute"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/server/adka2a/v2"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

const (
	sessionNameMaxLength = 20
)

// KAgentExecutorConfig holds the configuration for KAgentExecutor.
type KAgentExecutorConfig struct {
	RunnerConfig   runner.Config
	SessionService adksession.Service
	Stream         bool
	AppName        string
	Logger         *slog.Logger
	Output         *apiadk.OutputConfig
	Flush          func(context.Context) error
}

// KAgentExecutor keeps kagent's request/session glue around the upstream ADK
// A2A executor. Event conversion and artifact streaming are delegated to ADK.
type KAgentExecutor struct {
	builtin                 a2asrv.AgentExecutor
	sessionService          adksession.Service
	appName                 string
	logger                  *slog.Logger
	structuredOutputEnabled bool
	flush                   func(context.Context) error
}

type structuredOutput struct {
	schema *jsonschema.Resolved
	sha256 string
}

var _ a2asrv.AgentExecutor = (*KAgentExecutor)(nil)

// NewKAgentExecutor creates a KAgentExecutor from config. It returns an error if
// the configured output schema cannot be prepared for runtime validation.
func NewKAgentExecutor(cfg KAgentExecutorConfig) (*KAgentExecutor, error) {
	output, err := resolveStructuredOutput(cfg.Output)
	if err != nil {
		return nil, err
	}
	var runConfig adkagent.RunConfig
	if cfg.Stream {
		runConfig.StreamingMode = adkagent.StreamingModeSSE
	}
	runnerConfig := cfg.RunnerConfig
	if cfg.SessionService != nil {
		runnerConfig.SessionService = cfg.SessionService
	}
	if runnerConfig.Agent == nil {
		return nil, fmt.Errorf("root agent is required")
	}
	rootName := runnerConfig.Agent.Name()
	if rootName == "" {
		return nil, fmt.Errorf("root agent name is required")
	}
	logger := cfg.Logger.With("component", "kagent-executor")
	if usagePlugin, err := newTurnUsagePlugin(); err != nil {
		logger.Error("token usage aggregation is disabled", "error", err)
	} else {
		runnerConfig.PluginConfig.Plugins = append([]*plugin.Plugin{usagePlugin}, runnerConfig.PluginConfig.Plugins...)
	}
	builtin := adka2a.NewExecutor(adka2a.ExecutorConfig{
		RunnerConfig:       runnerConfig,
		RunConfig:          runConfig,
		A2APartConverter:   a2aPartConverter,
		GenAIPartConverter: structuredOutputPartConverter(output, rootName),
		AfterEventCallback: func(ctx adka2a.ExecutorContext, event *adksession.Event, processed *a2atype.TaskArtifactUpdateEvent) error {
			// Preserve the artifact's protocol type and assign kagent's ordering key.
			if processed.Artifact != nil {
				position := event.Timestamp
				if position.IsZero() {
					position = time.Now()
				}
				apia2a.SetTimelinePosition(processed.Artifact, position)
			}
			return transformStructuredOutput(output, rootName, event, processed)
		},
		// The aggregate rides on the terminal status update (completed,
		// input-required or failed), whose metadata a2a-go merges into the
		// stored task.
		AfterExecuteCallback: func(ctx adka2a.ExecutorContext, finalEvent *a2atype.TaskStatusUpdateEvent, _ error) error {
			turnUsageFrom(ctx).stampEvent(finalEvent)
			return nil
		},
		OutputMode: adka2a.OutputArtifactPerEvent,
	})

	return &KAgentExecutor{
		builtin:                 builtin,
		sessionService:          runnerConfig.SessionService,
		appName:                 cfg.AppName,
		logger:                  logger,
		structuredOutputEnabled: output != nil,
		flush:                   cfg.Flush,
	}, nil
}

// structuredOutputPartConverter drops partial root output before upstream ADK
// creates an A2A artifact. A structured response is not useful until it is a
// complete JSON value, and publishing fragments could persist invalid JSON.
// Events from tools and sub-agents retain the normal converter behavior.
func structuredOutputPartConverter(output *structuredOutput, rootName string) adka2a.GenAIPartConverter {
	return func(ctx context.Context, event *adksession.Event, part *genai.Part) (*a2atype.Part, error) {
		if output != nil && event != nil && event.Author == rootName && event.Partial {
			return nil, nil
		}
		return genAIPartConverter(ctx, event, part)
	}
}

func transformStructuredOutput(output *structuredOutput, rootName string, event *adksession.Event, processed *a2atype.TaskArtifactUpdateEvent) error {
	if output == nil || event.Author != rootName {
		return nil
	}
	// ADK treats interruption and skip-summarization events as final responses so
	// the current agent loop stops. They are not final structured results.
	if !event.IsFinalResponse() || len(event.LongRunningToolIDs) > 0 || event.Actions.SkipSummarization {
		return nil
	}
	switch event.FinishReason {
	case "", genai.FinishReasonUnspecified, genai.FinishReasonStop:
	default:
		return fmt.Errorf("output_validation_failed: root agent did not complete structured output")
	}
	value, err := finalStructuredOutput(event)
	if err != nil {
		return err
	}
	if err := output.schema.Validate(value); err != nil {
		return fmt.Errorf("output_validation_failed: root agent output does not conform to its schema")
	}
	if processed == nil || processed.Artifact == nil {
		return fmt.Errorf("output_validation_failed: root agent produced no result artifact")
	}
	part := apia2a.NewStructuredOutputPart(value, output.sha256)
	processed.Artifact.Parts = a2atype.ContentParts{part}
	return nil
}

// finalStructuredOutput returns ADK's parsed output when available. Normal A2A
// chat execution does not run the workflow-node wrapper that populates
// Event.Output, so in that path the final model text is decoded here before it
// is validated against the canonical JSON Schema.
func finalStructuredOutput(event *adksession.Event) (any, error) {
	if event.Output != nil {
		return event.Output, nil
	}
	if event.Content == nil {
		return nil, fmt.Errorf("output_validation_failed: root agent produced no structured value")
	}
	var text strings.Builder
	for _, part := range event.Content.Parts {
		if part == nil || part.Thought {
			continue
		}
		text.WriteString(part.Text)
	}
	if strings.TrimSpace(text.String()) == "" {
		return nil, fmt.Errorf("output_validation_failed: root agent produced no structured value")
	}
	var value any
	if err := json.Unmarshal([]byte(text.String()), &value); err != nil {
		return nil, fmt.Errorf("output_validation_failed: root agent output is not valid JSON")
	}
	return value, nil
}

// resolveStructuredOutput compiles the configured JSON Schema once so every
// execution can validate its final value with the same immutable validator.
func resolveStructuredOutput(config *apiadk.OutputConfig) (*structuredOutput, error) {
	if config == nil {
		return nil, nil
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(config.JSONSchema, &schema); err != nil {
		return nil, fmt.Errorf("decode output schema: %w", err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("resolve output schema: %w", err)
	}
	return &structuredOutput{schema: resolved, sha256: config.SHA256}, nil
}

// UserIDCallInterceptor returns an a2asrv.CallInterceptor that extracts the
// x-user-id HTTP header from the incoming request metadata and sets it as the
// authenticated user on the CallContext.
func UserIDCallInterceptor() a2asrv.CallInterceptor {
	return &userIDInterceptor{}
}

type userIDInterceptor struct {
	a2asrv.PassthroughCallInterceptor
}

func (u *userIDInterceptor) Before(ctx context.Context, callCtx *a2asrv.CallContext, _ *a2asrv.Request) (context.Context, any, error) {
	if callCtx == nil {
		return ctx, nil, nil
	}
	meta := callCtx.ServiceParams()
	if meta == nil {
		return ctx, nil, nil
	}
	vals, ok := meta.Get("x-user-id")
	if !ok || len(vals) == 0 || vals[0] == "" {
		return ctx, nil, nil
	}
	// Set the authenticated user so downstream code picks up the real identity.
	callCtx.User = a2asrv.NewAuthenticatedUser(vals[0], nil)
	return auth.WithUserID(ctx, vals[0]), nil, nil
}

// Execute applies kagent-specific request setup and delegates event generation
// to the upstream ADK executor, which streams output as artifact updates.
func (e *KAgentExecutor) Execute(ctx context.Context, reqCtx *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		if reqCtx.Message == nil {
			yield(nil, fmt.Errorf("A2A request message cannot be nil"))
			return
		}

		userID := "A2A_USER_" + reqCtx.ContextID
		trustedUserID := ""
		if callCtx, ok := a2asrv.CallContextFrom(ctx); ok && callCtx.User != nil && callCtx.User.Name != "" {
			userID = callCtx.User.Name
			trustedUserID = userID
		}
		sessionID := reqCtx.ContextID

		ctx = withBearerToken(ctx)
		ctx = auth.WithUserID(ctx, userID)

		// Resumed tasks (HITL cycles, follow-up messages) carry the previously
		// persisted total, so the usage total stays a task-lifetime sum.
		usage := &turnUsage{}
		usage.seedFromTask(reqCtx.StoredTask)
		ctx = withTurnUsage(ctx, usage)
		// The invocation span started before this executor ran, so the request
		// identity has to be recorded on it directly. ADK's own spans get it
		// through the request attribute span processor.
		resumed := reqCtx.StoredTask != nil &&
			(reqCtx.StoredTask.Status.State == a2atype.TaskStateInputRequired || reqCtx.StoredTask.Status.State == a2atype.TaskStateAuthRequired)
		tracing.InvocationFromContext(ctx).SetAttributes(tracing.RequestIdentity(sessionID, string(reqCtx.TaskID), resumed)...)
		ctx = telemetry.WithRequestAttributes(ctx, requestSpanAttributes(sessionID, string(reqCtx.TaskID), trustedUserID)...)

		e.logger.InfoContext(ctx, "execute",
			"task_id", reqCtx.TaskID,
			"context_id", reqCtx.ContextID,
			"app_name", e.appName,
			"user_id", userID,
		)

		// Run our own session management before upstream executor runs its prepareSession function.
		// This ensures that we create a session that contains metadata like x-kagent-source,
		// and the upstream executor will find this session already exists and skip creation.
		if err := e.ensureSession(ctx, reqCtx.Message, userID, sessionID); err != nil {
			yield(nil, err)
			return
		}

		hitlActivated := HitlActivated(ctx)
		if hitlActivated && IsHITLResponse(reqCtx.Message) {
			// a2a-go appends the inbound decision before invoking the executor. The
			// original decision is re-emitted once for history/audit, while the
			// transformed FunctionResponses are what ADK must consume.
			dropPreAppendedDecisionFromHistory(reqCtx.StoredTask, reqCtx.Message)
			decision := a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateWorking, reqCtx.Message)
			if !yield(decision, nil) {
				return
			}
			resumeMessage, err := BuildResumeHITLMessage(reqCtx.StoredTask, reqCtx.Message)
			if err != nil {
				yield(nil, err)
				return
			}
			reqCtx.Message = resumeMessage
		}

		structuredResultEmitted := false
		for event, err := range e.builtin.Execute(ctx, reqCtx) {
			// Mark that a structured result has been emitted
			if update, ok := event.(*a2atype.TaskArtifactUpdateEvent); ok && update.Artifact != nil &&
				slices.ContainsFunc(update.Artifact.Parts, apia2a.IsStructuredOutputPart) {
				structuredResultEmitted = true
			}
			// If the event is a task status update event and the status is completed,
			// but no structured result has been emitted, mark the status as failed with a message
			// indicating that the root agent produced no result artifact.
			if update, ok := event.(*a2atype.TaskStatusUpdateEvent); ok &&
				err == nil && e.structuredOutputEnabled && !structuredResultEmitted &&
				update.Status.State == a2atype.TaskStateCompleted {
				update.Status.State = a2atype.TaskStateFailed
				update.Status.Message = a2atype.NewMessageForTask(
					a2atype.MessageRoleAgent,
					update,
					a2atype.NewTextPart("output_validation_failed: root agent produced no result artifact"),
				)
			}
			// If the event is a task status update event and the status is input required, build the HITL status message
			if update, ok := event.(*a2atype.TaskStatusUpdateEvent); ok &&
				update.Status.State == a2atype.TaskStateInputRequired && update.Status.Message != nil {
				update.Status.Message = BuildHITLStatusMessage(update.Status.Message, hitlActivated)
				update.Status.Message.TaskID = update.TaskID
				update.Status.Message.ContextID = update.ContextID
			}
			// Status messages are outbound timeline items regardless of whether they
			// represent HITL, validation failures, or another runtime status.
			stampStatusMessageTimeline(event)
			canonicalizeADKEvent(event)
			if endsTurn(event, err) {
				e.flushTurnSpans(ctx)
			}
			if !yield(event, err) {
				return
			}
		}
	}
}

func stampStatusMessageTimeline(event a2atype.Event) {
	update, ok := event.(*a2atype.TaskStatusUpdateEvent)
	if !ok || update.Status.Message == nil {
		return
	}
	position := time.Now().UTC()
	if update.Status.Timestamp != nil {
		position = update.Status.Timestamp.UTC()
	}
	apia2a.SetTimelinePosition(update.Status.Message, position)
}

// requestSpanAttributes is the request identity every span of the turn
// carries. The user is set only when the gateway established one.
func requestSpanAttributes(conversationID, taskID, userID string) []attribute.KeyValue {
	attributes := make([]attribute.KeyValue, 0, 3)
	if conversationID != "" {
		attributes = append(attributes, attribute.String(tracing.AttributeConversationID, conversationID))
	}
	if taskID != "" {
		attributes = append(attributes, attribute.String(tracing.AttributeTaskID, taskID))
	}
	if userID != "" {
		attributes = append(attributes, attribute.String(tracing.AttributeUserID, userID))
	}
	return attributes
}

// endsTurn reports whether an event is the last one a turn produces: a terminal
// or waiting task state, or an error, after which the caller ends the stream.
func endsTurn(event a2atype.Event, err error) bool {
	if err != nil {
		return true
	}
	var state a2atype.TaskState
	switch e := event.(type) {
	case *a2atype.TaskStatusUpdateEvent:
		state = e.Status.State
	case *a2atype.Task:
		state = e.Status.State
	default:
		return false
	}
	return state.Terminal() || state == a2atype.TaskStateInputRequired || state == a2atype.TaskStateAuthRequired
}

// flushTurnSpans exports the turn's spans before the event that ends the turn
// leaves the process.
//
// The server-level flush runs once the handler returns. For a unary request that
// is before the response is written, so it is early enough. For a streaming
// request the terminal event has already been sent by then, and the gateway
// closes its stream to this runtime the moment it arrives; on Agent Substrate the
// actor is checkpointed right after, with the spans of every streamed turn still
// buffered and the flush's deadline expiring while the process is frozen. The
// only window that exists for a streamed turn is before that event is yielded.
func (e *KAgentExecutor) flushTurnSpans(ctx context.Context) {
	if e.flush == nil {
		return
	}
	if err := e.flush(ctx); err != nil {
		e.logger.ErrorContext(ctx, "failed to flush turn telemetry", "error", err)
	}
}

// ensureSession ensures that a session exists for the given user and session ID.
// If a session does not exist, it creates a new session with the given user and session ID.
func (e *KAgentExecutor) ensureSession(ctx context.Context, message *a2atype.Message, userID, sessionID string) error {
	if e.sessionService == nil {
		return nil
	}
	resp, err := e.sessionService.Get(ctx, &adksession.GetRequest{
		AppName: e.appName, UserID: userID, SessionID: sessionID,
	})
	if err == nil && resp != nil && resp.Session != nil {
		return nil
	}
	if err != nil {
		e.logger.DebugContext(ctx, "session lookup failed, will create", "error", err, "session_id", sessionID)
	}

	state := make(map[string]any)
	if sessionName := extractSessionName(message); sessionName != "" {
		state[StateKeySessionName] = sessionName
	}
	if callCtx, ok := a2asrv.CallContextFrom(ctx); ok {
		if meta := callCtx.ServiceParams(); meta != nil {
			if vals, ok := meta.Get("x-kagent-source"); ok && len(vals) > 0 && vals[0] != "" {
				state[StateKeySource] = vals[0]
			}
		}
	}
	if _, err := e.sessionService.Create(ctx, &adksession.CreateRequest{
		AppName: e.appName, UserID: userID, State: state, SessionID: sessionID,
	}); err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	return nil
}

// Cancel delegates cancellation to the upstream executor.
func (e *KAgentExecutor) Cancel(ctx context.Context, reqCtx *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		for event, err := range e.builtin.Cancel(ctx, reqCtx) {
			stampStatusMessageTimeline(event)
			canonicalizeADKEvent(event)
			if !yield(event, err) {
				return
			}
		}
	}
}

// Cleanup preserves the upstream executor's subagent cleanup behavior.
func (e *KAgentExecutor) Cleanup(ctx context.Context, reqCtx *a2asrv.ExecutorContext, result a2atype.SendMessageResult, cause error) {
	if cleaner, ok := e.builtin.(a2asrv.AgentExecutionCleaner); ok {
		cleaner.Cleanup(ctx, reqCtx, result, cause)
	}
}

// extractSessionName extracts session name from the first text part of a message.
func extractSessionName(message *a2atype.Message) string {
	if message == nil {
		return ""
	}
	for _, part := range message.Parts {
		if part == nil {
			continue
		}
		if text := part.Text(); text != "" {
			if len(text) > sessionNameMaxLength {
				return text[:sessionNameMaxLength] + "..."
			}
			return text
		}
	}
	return ""
}

// withBearerToken extracts the Bearer token from the incoming A2A request's
// Authorization header and stores it in ctx for API key passthrough.
func withBearerToken(ctx context.Context) context.Context {
	token := models.BearerFromCallContext(ctx)
	if token == "" {
		return ctx
	}
	return context.WithValue(ctx, models.BearerTokenKey, token)
}

// dropPreAppendedDecisionFromHistory removes a pre-appended HITL decision
// message inserted by a2a-go before executor invocation.
func dropPreAppendedDecisionFromHistory(task *a2atype.Task, incoming *a2atype.Message) {
	if task == nil || incoming == nil || len(task.History) == 0 {
		return
	}
	last := task.History[len(task.History)-1]
	if last == nil || last.ID != incoming.ID {
		return
	}
	if !IsHITLResponse(last) {
		return
	}
	task.History = task.History[:len(task.History)-1]
}
