package adk

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strings"
	"time"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go-adk/pkg/adk/converter"
	"github.com/kagent-dev/kagent/go-adk/pkg/adk/event"
	"github.com/kagent-dev/kagent/go-adk/pkg/core"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/a2a"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/session"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/skills"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/types"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"
)

// Compile-time interface compliance check
var _ core.Runner = (*ADKRunner)(nil)

// ADKRunner implements core.Runner for declarative agents (config-driven, lazy ADK initialization).
// Use NewADKRunner and pass the result to the executor; no runner factory needed.
//
// Event processing: The loop lives inside adk-go (Flow.Run). We only range over
// eventSeq. Adk-go runOneStep builds the LLM request from ctx.Session().Events().All();
// it never re-fetches the session. So the session in context must be updated when
// AppendEvent is called—otherwise the next runOneStep sees stale events (e.g. only user
// message) and the loop stops making progress after the first tool event. SessionServiceAdapter
// must append to SessionWrapper.session.Events on AppendEvent so the next runOneStep
// sees the new event (see session_adapter.go AppendEvent).
//
// runOneStep (adk-go internal/llminternal/base_flow.go) does one "model → tools → events" cycle:
//  1. preprocess(ctx, req): runs request processors; ContentsRequestProcessor fills req.Contents
//     from ctx.Session().Events().All() (user + model + tool events). So session must be up to date.
//  2. callLLM(ctx, req): runs BeforeModel callbacks, then f.Model.GenerateContent(ctx, req, stream);
//     yields each LLM response (streaming or final).
//  3. For the final resp: postprocess, then finalizeModelResponseEvent → yield modelResponseEvent.
//  4. handleFunctionCalls(ctx, tools, resp): for each function call in resp, finds tool, runs
//     tool.Run(toolCtx, args), builds a session.Event with FunctionResponse; merges all into one
//     event → yield merged tool-response event.
//  5. If ev.Actions.TransferToAgent is set, runs that agent and yields its events; else returns.
//
// Flow.Run then checks lastEvent.IsFinalResponse(); if false it calls runOneStep again (same ctx).
type ADKRunner struct {
	config          *types.AgentConfig
	skillsDirectory string
	adkRunner       *runner.Runner // Google ADK runner (lazy-initialized)
	logger          logr.Logger
}

// NewADKRunner creates a runner that uses Google ADK (lazy-initialized from config).
func NewADKRunner(config *types.AgentConfig, skillsDirectory string, logger logr.Logger) *ADKRunner {
	return &ADKRunner{
		config:          config,
		skillsDirectory: skillsDirectory,
		logger:          logger,
	}
}

// Run implements core.Runner.
//
// IMPORTANT: The caller MUST drain the returned channel to avoid goroutine leaks.
// The channel is closed when processing completes or context is cancelled.
func (r *ADKRunner) Run(ctx context.Context, args map[string]interface{}) (<-chan interface{}, error) {
	rargs := extractRunArgs(args)
	message := extractMessageFromArgs(args, r.logger)

	if r.skillsDirectory != "" && rargs.sessionID != "" {
		if _, err := skills.InitializeSessionPath(rargs.sessionID, r.skillsDirectory); err != nil {
			return nil, fmt.Errorf("failed to initialize session path: %w", err)
		}
	}

	if r.config == nil || r.config.Model == nil {
		return sendErrorEvent(&event.RunnerErrorEvent{
			ErrorCode: "NO_MODEL", ErrorMessage: "No model configured",
		}), nil
	}

	if message == nil {
		if r.logger.GetSink() != nil {
			r.logger.Info("Skipping LLM execution: message is nil", "configExists", r.config != nil, "modelExists", r.config.Model != nil)
		}
		return sendErrorEvent(&event.RunnerErrorEvent{
			ErrorCode: "NO_MESSAGE", ErrorMessage: fmt.Sprintf("No message provided (model: %s)", r.config.Model.GetType()),
		}), nil
	}

	// Lazy-initialize Google ADK runner
	appName, _ := args[a2a.ArgKeyAppName].(string)
	if r.adkRunner == nil {
		if r.logger.GetSink() != nil {
			r.logger.Info("Creating Google ADK Runner", "modelType", r.config.Model.GetType(), "sessionID", rargs.sessionID, "userID", rargs.userID, "appName", appName)
		}
		adkRunner, err := CreateGoogleADKRunner(r.config, rargs.sessionService, appName, r.logger)
		if err != nil {
			if r.logger.GetSink() != nil {
				r.logger.Error(err, "Failed to create Google ADK Runner")
			}
			return sendErrorEvent(&event.RunnerErrorEvent{
				ErrorCode: "RUNNER_INIT_ERROR", ErrorMessage: fmt.Sprintf("Error creating Google ADK Runner: %v", err),
			}), nil
		}
		r.adkRunner = adkRunner
	}

	if r.logger.GetSink() != nil {
		r.logger.Info("Executing agent with Google ADK Runner", "messageID", message.ID, "partsCount", len(message.Parts))
	}

	// Execute the Google ADK runner
	return r.runADK(ctx, message, args, rargs)
}

// runADK executes the Google ADK runner and streams events.
func (r *ADKRunner) runADK(ctx context.Context, message *a2atype.Message, args map[string]interface{}, rargs runArgs) (<-chan interface{}, error) {
	ch := make(chan interface{}, a2a.EventChannelBufferSize)

	go func() {
		defer close(ch)

		if (rargs.sessionService != nil && rargs.session == nil) || (rargs.session != nil && rargs.sessionService == nil) {
			if r.logger.GetSink() != nil {
				r.logger.Info("Session persistence may be skipped: session or session_service missing",
					"hasSession", rargs.session != nil, "hasSessionService", rargs.sessionService != nil)
			}
		}

		genaiContent, contentErr := converter.A2AMessageToGenAIContent(message)
		if contentErr != nil {
			if r.logger.GetSink() != nil {
				r.logger.Error(contentErr, "Failed to convert message to genai.Content")
			}
			ch <- &event.RunnerErrorEvent{
				ErrorCode: "CONVERSION_ERROR", ErrorMessage: fmt.Sprintf("Failed to convert message: %v", contentErr),
			}
			return
		}
		if genaiContent == nil || len(genaiContent.Parts) == 0 {
			if r.logger.GetSink() != nil {
				r.logger.Info("No message or empty parts in args")
			}
			return
		}

		runConfig := runConfigFromArgs(args)
		if r.logger.GetSink() != nil {
			r.logger.Info("Starting Google ADK runner", "userID", rargs.userID, "sessionID", rargs.sessionID, "hasContent", true)
		}

		// Runner context should have a long timeout for long-running MCP tools
		eventSeq := r.adkRunner.Run(ctx, rargs.userID, rargs.sessionID, genaiContent, runConfig)
		r.processEventLoop(ctx, ch, eventSeq, rargs)
	}()

	return ch, nil
}

// processEventLoop handles the ADK event iteration and dispatching.
func (r *ADKRunner) processEventLoop(
	ctx context.Context,
	ch chan<- interface{},
	eventSeq iter.Seq2[*adksession.Event, error],
	rargs runArgs,
) {
	eventCount := 0
	startTime := time.Now()
	lastEventTime := startTime

	for adkEvent, err := range eventSeq {
		eventCount++
		now := time.Now()
		timeSinceLastEvent := now.Sub(lastEventTime)
		totalElapsed := now.Sub(startTime)
		lastEventTime = now

		// Handle nil event (may occur on error)
		if adkEvent == nil {
			if err != nil {
				r.logError(err, "Google ADK yielded nil event with error", "eventNumber", eventCount)
				ch <- r.createErrorEvent(err)
			}
			continue
		}

		r.logEventTiming(eventCount, timeSinceLastEvent, totalElapsed, adkEvent)

		// Check context cancellation
		if ctx.Err() != nil {
			r.logError(ctx.Err(), "Runner context cancelled or timed out", "eventNumber", eventCount)
			ch <- r.createTimeoutEvent(ctx.Err())
			return
		}

		// Handle iteration error
		if err != nil {
			r.logError(err, "Error from Google ADK Runner", "eventNumber", eventCount)
			ch <- r.createErrorEvent(err)
			continue
		}

		if r.logger.GetSink() != nil {
			logADKEventDetails(r.logger, adkEvent, eventCount)
		}

		// Persist event to session
		r.persistEvent(ctx, adkEvent, eventCount, rargs)

		// Send event on channel
		if !r.sendEvent(ctx, ch, adkEvent, eventCount) {
			return
		}
	}

	r.logCompletion(eventCount, startTime)
}

func (r *ADKRunner) logError(err error, msg string, keysAndValues ...interface{}) {
	if r.logger.GetSink() != nil {
		r.logger.Error(err, msg, keysAndValues...)
	}
}

func (r *ADKRunner) logEventTiming(eventCount int, sinceLast, total time.Duration, adkEvent *adksession.Event) {
	if r.logger.GetSink() != nil {
		logADKEventTiming(r.logger, eventCount, sinceLast, total, adkEvent.Author, adkEvent.Partial)
	}
}

func (r *ADKRunner) createErrorEvent(err error) *event.RunnerErrorEvent {
	errorMessage, errorCode := formatRunnerError(err)
	return &event.RunnerErrorEvent{ErrorCode: errorCode, ErrorMessage: errorMessage}
}

func (r *ADKRunner) createTimeoutEvent(err error) *event.RunnerErrorEvent {
	msg := fmt.Sprintf("Google ADK runner timed out or was cancelled: %v", err)
	if err == context.DeadlineExceeded {
		msg += ". Long-running MCP tools may require a longer ExecutionTimeout (default 30m)."
	}
	return &event.RunnerErrorEvent{ErrorCode: "RUNNER_TIMEOUT", ErrorMessage: msg}
}

func (r *ADKRunner) persistEvent(ctx context.Context, adkEvent *adksession.Event, eventCount int, rargs runArgs) {
	shouldAppend := !adkEvent.Partial || event.EventHasToolContent(adkEvent)
	if rargs.sessionService == nil || rargs.session == nil || !shouldAppend {
		return
	}

	appendCtx, cancel := context.WithTimeout(context.Background(), a2a.EventPersistTimeout)
	defer cancel()

	if err := rargs.sessionService.AppendEvent(appendCtx, rargs.session, adkEvent); err != nil {
		r.logError(err, "Failed to append event to session", "eventNumber", eventCount, "author", adkEvent.Author)
	} else if r.logger.GetSink() != nil {
		r.logger.V(1).Info("Appended event to session", "eventNumber", eventCount, "author", adkEvent.Author)
	}
}

func (r *ADKRunner) sendEvent(ctx context.Context, ch chan<- interface{}, adkEvent *adksession.Event, eventCount int) bool {
	select {
	case ch <- adkEvent:
		if r.logger.GetSink() != nil {
			r.logger.V(1).Info("Sent event to channel", "eventNumber", eventCount, "author", adkEvent.Author)
		}
		return true
	case <-ctx.Done():
		if r.logger.GetSink() != nil {
			r.logger.Info("Context cancelled, stopping event processing")
		}
		return false
	}
}

func (r *ADKRunner) logCompletion(eventCount int, startTime time.Time) {
	if r.logger.GetSink() == nil {
		return
	}

	totalElapsed := time.Since(startTime)
	avgTime := time.Duration(0)
	if eventCount > 0 {
		avgTime = totalElapsed / time.Duration(eventCount)
	}

	r.logger.Info("Google ADK runner completed",
		"totalEvents", eventCount,
		"totalElapsed", totalElapsed,
		"averageTimePerEvent", avgTime)

	if eventCount == 0 {
		r.logger.Info("Google ADK runner completed with no events - this might indicate an issue")
	} else if totalElapsed < 1*time.Second && eventCount < 3 {
		r.logger.Info("Google ADK runner completed very quickly with few events - might have stopped prematurely",
			"eventCount", eventCount,
			"totalElapsed", totalElapsed)
	}
}

func extractMessageFromArgs(args map[string]interface{}, logger logr.Logger) *a2atype.Message {
	val := args[a2a.ArgKeyMessage]
	if val == nil {
		if logger.GetSink() != nil {
			keys := make([]string, 0, len(args))
			for k := range args {
				keys = append(keys, k)
			}
			logger.Info("No message found in args", "argsKeys", keys)
		}
		return nil
	}
	if msg, ok := val.(*a2atype.Message); ok {
		if logger.GetSink() != nil {
			logger.Info("Found message in args", "messageID", msg.ID, "role", msg.Role, "partsCount", len(msg.Parts))
		}
		return msg
	}
	if msg, ok := val.(a2atype.Message); ok {
		if logger.GetSink() != nil {
			logger.Info("Found message in args (non-pointer)", "messageID", msg.ID, "role", msg.Role, "partsCount", len(msg.Parts))
		}
		return &msg
	}
	if logger.GetSink() != nil {
		logger.Info("args[message] exists but wrong type", "type", fmt.Sprintf("%T", val))
	}
	return nil
}

// sendErrorEvent creates a buffered channel containing a single RunnerErrorEvent and closes it.
func sendErrorEvent(evt *event.RunnerErrorEvent) <-chan interface{} {
	ch := make(chan interface{}, 1)
	ch <- evt
	close(ch)
	return ch
}

// runArgs holds extracted run arguments from args map.
type runArgs struct {
	userID         string
	sessionID      string
	sessionService session.SessionService
	session        *session.Session
}

func extractRunArgs(args map[string]interface{}) runArgs {
	var r runArgs
	if uid, ok := args[a2a.ArgKeyUserID].(string); ok {
		r.userID = uid
	}
	if sid, ok := args[a2a.ArgKeySessionID].(string); ok {
		r.sessionID = sid
	}
	if svc, ok := args[a2a.ArgKeySessionService].(session.SessionService); ok {
		r.sessionService = svc
	}
	if s, ok := args[a2a.ArgKeySession].(*session.Session); ok {
		r.session = s
	}
	return r
}

func runConfigFromArgs(args map[string]interface{}) agent.RunConfig {
	cfg := agent.RunConfig{}
	if m, ok := args[a2a.ArgKeyRunConfig].(map[string]interface{}); ok {
		if stream, ok := m[a2a.RunConfigKeyStreamingMode].(string); ok && stream == "SSE" {
			cfg.StreamingMode = agent.StreamingModeSSE
		}
	}
	return cfg
}

// errorClassification defines how to classify and format an error.
type errorClassification struct {
	code     string
	template string
}

// errorPatterns maps error patterns to their classification.
// Patterns are checked in order; first match wins. All pattern strings are pre-lowered.
var errorPatterns = []struct {
	patterns       []string
	classification errorClassification
}{
	{
		patterns: []string{
			"failed to extract tools",
			"failed to get mcp session",
			"failed to init mcp session",
			"connection failed",
			"context deadline exceeded",
			"client.timeout exceeded",
		},
		classification: errorClassification{
			code: "MCP_CONNECTION_ERROR",
			template: "MCP connection failure or timeout. This can happen if the MCP server is unreachable or slow to respond. " +
				"Please verify your MCP server is running and accessible. Original error: %s",
		},
	},
	{
		patterns: []string{
			"name or service not known",
			"no such host",
			"dns",
		},
		classification: errorClassification{
			code: "MCP_DNS_ERROR",
			template: "DNS resolution failure for MCP server: %s. " +
				"Please check if the MCP server address is correct and reachable within the cluster.",
		},
	},
	{
		patterns: []string{
			"connection refused",
			"connect: connection refused",
			"econnrefused",
		},
		classification: errorClassification{
			code: "MCP_CONNECTION_REFUSED",
			template: "Failed to connect to MCP server: %s. " +
				"The server might be down or blocked by network policies.",
		},
	},
}

// formatRunnerError returns a user-facing error message and code for runner errors.
func formatRunnerError(err error) (errorMessage, errorCode string) {
	if err == nil {
		return "", ""
	}

	errStr := err.Error()
	lowerErr := strings.ToLower(errStr)

	for _, ep := range errorPatterns {
		if matchesAnyPattern(lowerErr, ep.patterns) {
			return fmt.Sprintf(ep.classification.template, errStr), ep.classification.code
		}
	}

	return errStr, "RUNNER_ERROR"
}

// matchesAnyPattern checks if the lowered string contains any of the pre-lowered patterns.
func matchesAnyPattern(lowerStr string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.Contains(lowerStr, pattern) {
			return true
		}
	}
	return false
}

func logADKEventTiming(logger logr.Logger, eventCount int, timeSinceLastEvent, totalElapsed time.Duration, author string, partial bool) {
	logger.V(1).Info("Processing Google ADK event",
		"eventNumber", eventCount,
		"timeSinceLastEvent", timeSinceLastEvent,
		"totalElapsed", totalElapsed,
		"author", author,
		"partial", partial)
	if timeSinceLastEvent > 30*time.Second && eventCount > 1 {
		logger.Info("Long delay between events - may be executing tool",
			"timeSinceLastEvent", timeSinceLastEvent, "eventNumber", eventCount)
	}
}

func logADKEventDetails(logger logr.Logger, e *adksession.Event, eventCount int) {
	if e == nil {
		logger.V(1).Info("Google ADK event received (nil)", "eventNumber", eventCount)
		return
	}
	if e.LLMResponse.Content == nil || e.LLMResponse.Content.Parts == nil {
		logger.V(1).Info("Google ADK event received", "eventNumber", eventCount, "author", e.Author, "partial", e.Partial)
		return
	}
	hasTool := false
	for _, part := range e.LLMResponse.Content.Parts {
		if part.FunctionCall != nil {
			hasTool = true
			logFunctionCall(logger, part, eventCount)
		}
		if part.FunctionResponse != nil {
			hasTool = true
			logFunctionResponse(logger, part, eventCount, e.Partial)
		}
	}
	if !hasTool {
		logger.V(1).Info("Google ADK event received", "eventNumber", eventCount, "author", e.Author, "partial", e.Partial,
			"hasContent", true, "partsCount", len(e.LLMResponse.Content.Parts))
	}
}

// jsonOrFallback marshals v to JSON, falling back to Sprintf on error.
func jsonOrFallback(v interface{}) string {
	if v == nil {
		return ""
	}
	if b, err := json.Marshal(v); err == nil {
		return string(b)
	}
	return fmt.Sprintf("%v", v)
}

func logFunctionCall(logger logr.Logger, part *genai.Part, eventCount int) {
	logger.Info("MCP function call", "tool", part.FunctionCall.Name, "callID", part.FunctionCall.ID)
	argsJSON := jsonOrFallback(part.FunctionCall.Args)
	logger.V(1).Info("Google ADK event contains function call",
		"eventNumber", eventCount, "functionName", part.FunctionCall.Name,
		"functionID", part.FunctionCall.ID, "args", argsJSON)
}

func logFunctionResponse(logger logr.Logger, part *genai.Part, eventCount int, partial bool) {
	responseBody := jsonOrFallback(part.FunctionResponse.Response)
	if len(responseBody) > a2a.ResponseBodyMaxLength {
		responseBody = responseBody[:a2a.ResponseBodyMaxLength] + "... (truncated)"
	}
	logger.Info("MCP function response", "tool", part.FunctionResponse.Name,
		"callID", part.FunctionResponse.ID, "responseLength", len(responseBody))
	logger.V(1).Info("Google ADK event contains function response",
		"eventNumber", eventCount, "functionName", part.FunctionResponse.Name,
		"functionID", part.FunctionResponse.ID, "responseLength", len(responseBody), "partial", partial)
}
