package a2a

import (
	"context"
	"fmt"
	"iter"
	"strings"
	"unicode/utf8"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/adk/pkg/auth"
	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	"github.com/kagent-dev/kagent/go/adk/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/server/adka2a/v2"
	adksession "google.golang.org/adk/v2/session"
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
	Logger         logr.Logger
}

// KAgentExecutor keeps kagent's request/session glue around the upstream ADK
// A2A executor. Event conversion and artifact streaming are delegated to ADK.
type KAgentExecutor struct {
	builtin        a2asrv.AgentExecutor
	sessionService adksession.Service
	appName        string
	logger         logr.Logger
}

var _ a2asrv.AgentExecutor = (*KAgentExecutor)(nil)

// NewKAgentExecutor creates a KAgentExecutor from config.
func NewKAgentExecutor(cfg KAgentExecutorConfig) *KAgentExecutor {
	var runConfig adkagent.RunConfig
	if cfg.Stream {
		runConfig.StreamingMode = adkagent.StreamingModeSSE
	}
	runnerConfig := cfg.RunnerConfig
	if cfg.SessionService != nil {
		runnerConfig.SessionService = cfg.SessionService
	}
	builtin := adka2a.NewExecutor(adka2a.ExecutorConfig{
		RunnerConfig:       runnerConfig,
		RunConfig:          runConfig,
		A2APartConverter:   a2aPartConverter,
		GenAIPartConverter: genAIPartConverter,
		AfterEventCallback: func(ctx adka2a.ExecutorContext, event *adksession.Event, _ *a2atype.TaskArtifactUpdateEvent) error {
			if event.InvocationID != "" {
				trace.SpanFromContext(ctx).SetAttributes(attribute.String("gcp.vertex.agent.invocation_id", event.InvocationID))
			}
			return nil
		},
		OutputMode: adka2a.OutputArtifactPerEvent,
	})

	return &KAgentExecutor{
		builtin:        builtin,
		sessionService: runnerConfig.SessionService,
		appName:        cfg.AppName,
		logger:         cfg.Logger.WithName("kagent-executor"),
	}
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
	return ctx, nil, nil
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
		if callCtx, ok := a2asrv.CallContextFrom(ctx); ok && callCtx.User != nil && callCtx.User.Name != "" {
			userID = callCtx.User.Name
		}
		sessionID := reqCtx.ContextID

		ctx = withBearerToken(ctx)
		ctx = auth.WithUserID(ctx, userID)
		spanAttributes := map[string]string{
			"kagent.user_id":         userID,
			"gen_ai.task.id":         string(reqCtx.TaskID),
			"gen_ai.conversation.id": sessionID,
		}
		if e.appName != "" {
			spanAttributes["kagent.app_name"] = e.appName
		}
		ctx = telemetry.SetKAgentSpanAttributes(ctx, spanAttributes)
		ctx, invocationSpan := telemetry.StartInvocationSpan(ctx)
		defer invocationSpan.End()
		telemetry.SetMessageMetadataAttributes(ctx, reqCtx.Message.Metadata)

		e.logger.Info("Execute",
			"taskID", reqCtx.TaskID,
			"contextID", reqCtx.ContextID,
			"appName", e.appName,
			"userID", userID,
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

		for event, err := range e.builtin.Execute(ctx, reqCtx) {
			// If the event is a task status update event and the status is input required, build the HITL status message
			if update, ok := event.(*a2atype.TaskStatusUpdateEvent); ok &&
				update.Status.State == a2atype.TaskStateInputRequired && update.Status.Message != nil {
				update.Status.Message = BuildHITLStatusMessage(update.Status.Message, hitlActivated)
				update.Status.Message.TaskID = update.TaskID
				update.Status.Message.ContextID = update.ContextID
			}
			if !yield(event, err) {
				return
			}
		}
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
		e.logger.V(1).Info("Session lookup failed, will create", "error", err, "sessionID", sessionID)
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
	return e.builtin.Cancel(ctx, reqCtx)
}

// Cleanup preserves the upstream executor's subagent cleanup behavior.
func (e *KAgentExecutor) Cleanup(ctx context.Context, reqCtx *a2asrv.ExecutorContext, result a2atype.SendMessageResult, cause error) {
	if cleaner, ok := e.builtin.(a2asrv.AgentExecutionCleaner); ok {
		cleaner.Cleanup(ctx, reqCtx, result, cause)
	}
}

// extractSessionName extracts session name from the first text part of a message.
// Truncation is rune-aware: cutting on a byte boundary can split a multi-byte
// UTF-8 rune and yield an invalid-UTF-8 name, which the Postgres session store
// rejects on session create. sessionNameMaxLength is therefore counted in runes.
func extractSessionName(message *a2atype.Message) string {
	if message == nil {
		return ""
	}
	for _, part := range message.Parts {
		if part == nil {
			continue
		}
		if text := part.Text(); text != "" {
			if utf8.RuneCountInString(text) > sessionNameMaxLength {
				return string([]rune(text)[:sessionNameMaxLength]) + "..."
			}
			return text
		}
	}
	return ""
}

// withBearerToken extracts the Bearer token from the incoming A2A request's
// Authorization header and stores it in ctx for API key passthrough.
func withBearerToken(ctx context.Context) context.Context {
	callCtx, ok := a2asrv.CallContextFrom(ctx)
	if !ok {
		return ctx
	}
	meta := callCtx.ServiceParams()
	if meta == nil {
		return ctx
	}
	vals, ok := meta.Get("authorization")
	if !ok || len(vals) == 0 || vals[0] == "" {
		return ctx
	}
	parts := strings.Fields(strings.TrimSpace(vals[0]))
	if len(parts) >= 2 && strings.EqualFold(parts[0], "Bearer") {
		return context.WithValue(ctx, models.BearerTokenKey, parts[1])
	}
	return ctx
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
