package adk

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go-adk/pkg/core"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// GoogleADKRunnerWrapper wraps Google ADK Runner to match our Runner interface.
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
type GoogleADKRunnerWrapper struct {
	runner *runner.Runner
	logger logr.Logger
}

// NewGoogleADKRunnerWrapper creates a new wrapper
func NewGoogleADKRunnerWrapper(adkRunner *runner.Runner, logger logr.Logger) *GoogleADKRunnerWrapper {
	return &GoogleADKRunnerWrapper{
		runner: adkRunner,
		logger: logger,
	}
}

// runArgs holds extracted run arguments from args map.
type runArgs struct {
	userID         string
	sessionID      string
	sessionService core.SessionService
	session        *core.Session
}

func extractRunArgs(args map[string]interface{}) runArgs {
	var r runArgs
	if uid, ok := args[ArgKeyUserID].(string); ok {
		r.userID = uid
	}
	if sid, ok := args[ArgKeySessionID].(string); ok {
		r.sessionID = sid
	}
	if svc, ok := args[ArgKeySessionService].(core.SessionService); ok {
		r.sessionService = svc
	}
	if s, ok := args[ArgKeySession].(*core.Session); ok {
		r.session = s
	} else if wrapper, ok := args[ArgKeySession].(*SessionWrapper); ok {
		r.session = wrapper.session
	}
	return r
}

func buildGenAIContentFromArgs(args map[string]interface{}) (*genai.Content, error) {
	if newMsg, ok := args[ArgKeyNewMessage].(map[string]interface{}); ok && hasNonEmptyParts(newMsg) {
		return convertMapToGenAIContent(newMsg)
	}
	if msg, ok := args[ArgKeyMessage].(*protocol.Message); ok {
		return convertProtocolMessageToGenAIContent(msg)
	}
	return nil, nil
}

func runConfigFromArgs(args map[string]interface{}) agent.RunConfig {
	cfg := agent.RunConfig{}
	if m, ok := args[ArgKeyRunConfig].(map[string]interface{}); ok {
		if stream, ok := m[core.RunConfigKeyStreamingMode].(string); ok && stream == "SSE" {
			cfg.StreamingMode = agent.StreamingModeSSE
		}
	}
	return cfg
}

// Run implements our Runner interface by converting between formats.
// Aligned with runner.go AgentRunner.Run: same channel size, context usage, session append pattern, and channel send semantics.
func (w *GoogleADKRunnerWrapper) Run(ctx context.Context, args map[string]interface{}) (<-chan interface{}, error) {
	ch := make(chan interface{}, 10)

	go func() {
		defer close(ch)

		rargs := extractRunArgs(args)
		if (rargs.sessionService != nil && rargs.session == nil) || (rargs.session != nil && rargs.sessionService == nil) {
			if w.logger.GetSink() != nil {
				w.logger.Info("Session persistence may be skipped: session or session_service missing",
					"hasSession", rargs.session != nil, "hasSessionService", rargs.sessionService != nil)
			}
		}

		genaiContent, contentErr := buildGenAIContentFromArgs(args)
		if contentErr != nil {
			if w.logger.GetSink() != nil {
				w.logger.Error(contentErr, "Failed to convert message to genai.Content")
			}
			ch <- &RunnerErrorEvent{
				ErrorCode: "CONVERSION_ERROR", ErrorMessage: fmt.Sprintf("Failed to convert message: %v", contentErr),
			}
			return
		}
		if genaiContent == nil || len(genaiContent.Parts) == 0 {
			if w.logger.GetSink() != nil {
				w.logger.Info("No message or empty parts in args")
			}
			return
		}

		runConfig := runConfigFromArgs(args)
		if w.logger.GetSink() != nil {
			w.logger.Info("Starting Google ADK runner", "userID", rargs.userID, "sessionID", rargs.sessionID, "hasContent", true)
		}

		// Runner context should have a long timeout for long-running MCP tools; the executor
		// uses context.WithoutCancel so execution gets full ExecutionTimeout regardless of request cancel.
		eventSeq := w.runner.Run(ctx, rargs.userID, rargs.sessionID, genaiContent, runConfig)

		// Convert Google ADK events to our Event format
		// The iterator will yield events as Google ADK processes the conversation,
		// including tool execution events automatically
		// NOTE: The iterator may block while Google ADK executes tools internally.
		// This is expected behavior - tools may take time to execute.
		eventCount := 0
		startTime := time.Now()
		lastEventTime := startTime
		for adkEvent, err := range eventSeq {
			eventCount++
			now := time.Now()
			timeSinceLastEvent := now.Sub(lastEventTime)
			totalElapsed := now.Sub(startTime)
			lastEventTime = now

			// Iterator may yield nil event (e.g. on error); avoid nil dereference
			if adkEvent == nil {
				if err != nil {
					if w.logger.GetSink() != nil {
						w.logger.Error(err, "Google ADK yielded nil event with error", "eventNumber", eventCount)
					}
					errorMessage, errorCode := formatRunnerError(err)
					ch <- &RunnerErrorEvent{
						ErrorCode:    errorCode,
						ErrorMessage: errorMessage,
					}
				}
				continue
			}

			if w.logger.GetSink() != nil {
				logADKEventTiming(w.logger, eventCount, timeSinceLastEvent, totalElapsed, getEventAuthor(adkEvent), getEventPartial(adkEvent))
			}

			if ctx.Err() != nil {
				if w.logger.GetSink() != nil {
					w.logger.Error(ctx.Err(), "Runner context cancelled or timed out", "eventNumber", eventCount)
				}
				msg := fmt.Sprintf("Google ADK runner timed out or was cancelled: %v", ctx.Err())
				if ctx.Err() == context.DeadlineExceeded {
					msg += ". Long-running MCP tools may require a longer ExecutionTimeout (default 30m)."
				}
				ch <- &RunnerErrorEvent{
					ErrorCode:    "RUNNER_TIMEOUT",
					ErrorMessage: msg,
				}
				return
			}
			if err != nil {
				if w.logger.GetSink() != nil {
					w.logger.Error(err, "Error from Google ADK Runner", "eventNumber", eventCount)
				}
				errorMessage, errorCode := formatRunnerError(err)
				ch <- &RunnerErrorEvent{
					ErrorCode:    errorCode,
					ErrorMessage: errorMessage,
				}
				continue
			}

			if w.logger.GetSink() != nil {
				logADKEventDetails(w.logger, adkEvent, eventCount)
			}

			shouldAppend := !adkEvent.Partial || EventHasToolContent(adkEvent)
			if rargs.sessionService != nil && rargs.session != nil && shouldAppend {
				appendCtx, appendCancel := context.WithTimeout(context.Background(), 30*time.Second)
				if err := rargs.sessionService.AppendEvent(appendCtx, rargs.session, adkEvent); err != nil {
					if w.logger.GetSink() != nil {
						w.logger.Error(err, "Failed to append event to session", "eventNumber", eventCount, "author", adkEvent.Author)
					}
				} else if w.logger.GetSink() != nil {
					w.logger.V(1).Info("Appended event to session", "eventNumber", eventCount, "author", adkEvent.Author)
				}
				appendCancel()
			}

			// Send event on channel (aligned with runner.go: select with ctx.Done(), no default)
			select {
			case ch <- adkEvent:
				if w.logger.GetSink() != nil {
					w.logger.V(1).Info("Sent event to channel", "eventNumber", eventCount, "author", adkEvent.Author)
				}
			case <-ctx.Done():
				if w.logger.GetSink() != nil {
					w.logger.Info("Context cancelled, stopping event processing")
				}
				return
			}
		}

		// Iterator completed - log final event count
		if w.logger.GetSink() != nil {
			totalElapsed := time.Since(startTime)
			w.logger.Info("Google ADK runner completed",
				"totalEvents", eventCount,
				"totalElapsed", totalElapsed,
				"averageTimePerEvent", func() time.Duration {
					if eventCount > 0 {
						return totalElapsed / time.Duration(eventCount)
					}
					return 0
				}())

			// Check if we stopped prematurely (might indicate a hang or error)
			if eventCount == 0 {
				w.logger.Info("Google ADK runner completed with no events - this might indicate an issue")
			} else if totalElapsed < 1*time.Second && eventCount < 3 {
				w.logger.Info("Google ADK runner completed very quickly with few events - might have stopped prematurely",
					"eventCount", eventCount,
					"totalElapsed", totalElapsed)
			}
		}
	}()

	return ch, nil
}

// convertProtocolMessageToGenAIContent converts protocol.Message to genai.Content
func convertProtocolMessageToGenAIContent(msg *protocol.Message) (*genai.Content, error) {
	if msg == nil {
		return nil, fmt.Errorf("message is nil")
	}

	parts := make([]*genai.Part, 0, len(msg.Parts))
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case *protocol.TextPart:
			parts = append(parts, genai.NewPartFromText(p.Text))
		case *protocol.FilePart:
			if p.File != nil {
				if uriFile, ok := p.File.(*protocol.FileWithURI); ok {
					// Convert FileWithURI to genai.Part with file_data
					mimeType := ""
					if uriFile.MimeType != nil {
						mimeType = *uriFile.MimeType
					}
					parts = append(parts, genai.NewPartFromURI(uriFile.URI, mimeType))
				} else if bytesFile, ok := p.File.(*protocol.FileWithBytes); ok {
					// Convert FileWithBytes to genai.Part with inline_data
					data, err := base64.StdEncoding.DecodeString(bytesFile.Bytes)
					if err != nil {
						return nil, fmt.Errorf("failed to decode base64 file data: %w", err)
					}
					mimeType := ""
					if bytesFile.MimeType != nil {
						mimeType = *bytesFile.MimeType
					}
					parts = append(parts, genai.NewPartFromBytes(data, mimeType))
				}
			}
		case *protocol.DataPart:
			// Check metadata for special types (function calls, responses, etc.)
			if p.Metadata != nil {
				if partType, ok := p.Metadata[core.GetKAgentMetadataKey(core.A2ADataPartMetadataTypeKey)].(string); ok {
					switch partType {
					case core.A2ADataPartMetadataTypeFunctionCall:
						// Convert function call data to genai.Part
						if funcCallData, ok := p.Data.(map[string]interface{}); ok {
							name, _ := funcCallData["name"].(string)
							args, _ := funcCallData["args"].(map[string]interface{})
							if name != "" {
								genaiPart := genai.NewPartFromFunctionCall(name, args)
								if id, ok := funcCallData["id"].(string); ok && id != "" {
									genaiPart.FunctionCall.ID = id
								}
								parts = append(parts, genaiPart)
							}
						}
					case core.A2ADataPartMetadataTypeFunctionResponse:
						// Convert function response data to genai.Part
						if funcRespData, ok := p.Data.(map[string]interface{}); ok {
							name, _ := funcRespData["name"].(string)
							response, _ := funcRespData["response"].(map[string]interface{})
							if name != "" {
								genaiPart := genai.NewPartFromFunctionResponse(name, response)
								if id, ok := funcRespData["id"].(string); ok && id != "" {
									genaiPart.FunctionResponse.ID = id
								}
								parts = append(parts, genaiPart)
							}
						}
					default:
						// For other DataPart types, convert to JSON text
						dataJSON, err := json.Marshal(p.Data)
						if err == nil {
							parts = append(parts, genai.NewPartFromText(string(dataJSON)))
						}
					}
					continue
				}
			}
			// Default: convert DataPart to JSON text
			dataJSON, err := json.Marshal(p.Data)
			if err == nil {
				parts = append(parts, genai.NewPartFromText(string(dataJSON)))
			}
		}
	}

	role := "user"
	if msg.Role == protocol.MessageRoleAgent {
		role = "model"
	}

	return &genai.Content{
		Role:  role,
		Parts: parts,
	}, nil
}

// hasNonEmptyParts returns true if the map has a "parts" key with a non-empty slice.
// Handles both []interface{} (JSON) and []map[string]interface{} (from ConvertA2ARequestToRunArgs).
func hasNonEmptyParts(msgMap map[string]interface{}) bool {
	partsVal, exists := msgMap[core.PartKeyParts]
	if !exists || partsVal == nil {
		return false
	}
	if partsList, ok := partsVal.([]interface{}); ok {
		return len(partsList) > 0
	}
	if partsList, ok := partsVal.([]map[string]interface{}); ok {
		return len(partsList) > 0
	}
	return false
}

// convertMapToGenAIContent converts a map (from Python new_message format or ConvertA2ARequestToRunArgs) to genai.Content.
// Handles parts as []interface{} (JSON) or []map[string]interface{} (from Go).
func convertMapToGenAIContent(msgMap map[string]interface{}) (*genai.Content, error) {
	role, _ := msgMap[core.PartKeyRole].(string)
	if role == "" {
		role = "user"
	}

	// Handle parts - []interface{} (JSON) or []map[string]interface{} (from ConvertA2ARequestToRunArgs)
	var partsInterface []interface{}
	if partsVal, exists := msgMap[core.PartKeyParts]; exists && partsVal != nil {
		if partsList, ok := partsVal.([]interface{}); ok {
			partsInterface = partsList
		} else if partsList, ok := partsVal.([]map[string]interface{}); ok {
			// From Go: ConvertA2ARequestToRunArgs sets parts as []map[string]interface{}
			for i := range partsList {
				partsInterface = append(partsInterface, partsList[i])
			}
		}
	}

	parts := make([]*genai.Part, 0, len(partsInterface))
	for _, partInterface := range partsInterface {
		if partMap, ok := partInterface.(map[string]interface{}); ok {
			// Handle text parts
			if text, ok := partMap[core.PartKeyText].(string); ok {
				parts = append(parts, genai.NewPartFromText(text))
				continue
			}
			// Handle function calls
			if functionCall, ok := partMap[core.PartKeyFunctionCall].(map[string]interface{}); ok {
				name, _ := functionCall[core.PartKeyName].(string)
				args, _ := functionCall[core.PartKeyArgs].(map[string]interface{})
				if name != "" {
					genaiPart := genai.NewPartFromFunctionCall(name, args)
					if id, ok := functionCall[core.PartKeyID].(string); ok && id != "" {
						genaiPart.FunctionCall.ID = id
					}
					parts = append(parts, genaiPart)
				}
				continue
			}
			// Handle function responses
			if functionResponse, ok := partMap[core.PartKeyFunctionResponse].(map[string]interface{}); ok {
				name, _ := functionResponse[core.PartKeyName].(string)
				response, _ := functionResponse[core.PartKeyResponse].(map[string]interface{})
				if name != "" {
					genaiPart := genai.NewPartFromFunctionResponse(name, response)
					if id, ok := functionResponse[core.PartKeyID].(string); ok && id != "" {
						genaiPart.FunctionResponse.ID = id
					}
					parts = append(parts, genaiPart)
				}
				continue
			}
			// Handle file_data
			if fileData, ok := partMap[core.PartKeyFileData].(map[string]interface{}); ok {
				if uri, ok := fileData[core.PartKeyFileURI].(string); ok {
					mimeType, _ := fileData[core.PartKeyMimeType].(string)
					parts = append(parts, genai.NewPartFromURI(uri, mimeType))
				}
				continue
			}
			// Handle inline_data
			if inlineData, ok := partMap[core.PartKeyInlineData].(map[string]interface{}); ok {
				var data []byte
				if dataBytes, ok := inlineData["data"].([]byte); ok {
					data = dataBytes
				} else if dataStr, ok := inlineData["data"].(string); ok {
					// Try to decode base64 if it's a string
					if decoded, err := base64.StdEncoding.DecodeString(dataStr); err == nil {
						data = decoded
					} else {
						data = []byte(dataStr)
					}
				}
				if len(data) > 0 {
					mimeType, _ := inlineData[core.PartKeyMimeType].(string)
					parts = append(parts, genai.NewPartFromBytes(data, mimeType))
				}
				continue
			}
		}
	}

	return &genai.Content{
		Role:  role,
		Parts: parts,
	}, nil
}

// formatRunnerError returns a user-facing error message and code for runner errors.
func formatRunnerError(err error) (errorMessage, errorCode string) {
	if err == nil {
		return "", ""
	}
	errorMessage = err.Error()
	errorCode = "RUNNER_ERROR"
	if containsAny(errorMessage, []string{
		"failed to extract tools",
		"failed to get MCP session",
		"failed to init MCP session",
		"connection failed",
		"context deadline exceeded",
		"Client.Timeout exceeded",
	}) {
		errorCode = "MCP_CONNECTION_ERROR"
		errorMessage = fmt.Sprintf(
			"MCP connection failure or timeout. This can happen if the MCP server is unreachable or slow to respond. "+
				"Please verify your MCP server is running and accessible. Original error: %s",
			err.Error(),
		)
	} else if containsAny(errorMessage, []string{
		"Name or service not known",
		"no such host",
		"DNS",
	}) {
		errorCode = "MCP_DNS_ERROR"
		errorMessage = fmt.Sprintf(
			"DNS resolution failure for MCP server: %s. "+
				"Please check if the MCP server address is correct and reachable within the cluster.",
			err.Error(),
		)
	} else if containsAny(errorMessage, []string{
		"Connection refused",
		"connect: connection refused",
		"ECONNREFUSED",
	}) {
		errorCode = "MCP_CONNECTION_REFUSED"
		errorMessage = fmt.Sprintf(
			"Failed to connect to MCP server: %s. "+
				"The server might be down or blocked by network policies.",
			err.Error(),
		)
	}
	return errorMessage, errorCode
}

// containsAny checks if the string contains any of the substrings (case-insensitive).
func containsAny(s string, substrings []string) bool {
	lowerS := strings.ToLower(s)
	for _, substr := range substrings {
		if strings.Contains(lowerS, strings.ToLower(substr)) {
			return true
		}
	}
	return false
}

func getEventAuthor(event interface{}) string {
	if e, ok := event.(*adksession.Event); ok {
		return e.Author
	}
	return ""
}

func getEventPartial(event interface{}) bool {
	if e, ok := event.(*adksession.Event); ok {
		return e.Partial
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

func logADKEventDetails(logger logr.Logger, event interface{}, eventCount int) {
	e, ok := event.(*adksession.Event)
	if !ok || e.LLMResponse.Content == nil {
		logger.V(1).Info("Google ADK event received", "eventNumber", eventCount, "author", getEventAuthor(event), "partial", getEventPartial(event))
		return
	}
	hasTool := false
	for _, part := range e.LLMResponse.Content.Parts {
		if part.FunctionCall != nil {
			hasTool = true
			argsJSON := ""
			if part.FunctionCall.Args != nil {
				if b, err := json.Marshal(part.FunctionCall.Args); err == nil {
					argsJSON = string(b)
				} else {
					argsJSON = fmt.Sprintf("%v", part.FunctionCall.Args)
				}
			}
			logger.Info("MCP function call", "tool", part.FunctionCall.Name, "callID", part.FunctionCall.ID)
			logger.V(1).Info("Google ADK event contains function call",
				"eventNumber", eventCount, "functionName", part.FunctionCall.Name, "functionID", part.FunctionCall.ID, "args", argsJSON)
		}
		if part.FunctionResponse != nil {
			hasTool = true
			responseBody := ""
			if part.FunctionResponse.Response != nil {
				if b, err := json.Marshal(part.FunctionResponse.Response); err == nil {
					responseBody = string(b)
				} else {
					responseBody = fmt.Sprintf("%v", part.FunctionResponse.Response)
				}
				if len(responseBody) > 2000 {
					responseBody = responseBody[:2000] + "... (truncated)"
				}
			}
			logger.Info("MCP function response", "tool", part.FunctionResponse.Name, "callID", part.FunctionResponse.ID, "responseLength", len(responseBody))
			logger.V(1).Info("Google ADK event contains function response",
				"eventNumber", eventCount, "functionName", part.FunctionResponse.Name, "functionID", part.FunctionResponse.ID, "responseLength", len(responseBody), "partial", e.Partial)
		}
	}
	if !hasTool {
		partsCount := 0
		if e.LLMResponse.Content != nil {
			partsCount = len(e.LLMResponse.Content.Parts)
		}
		logger.V(1).Info("Google ADK event received", "eventNumber", eventCount, "author", e.Author, "partial", e.Partial, "hasContent", true, "partsCount", partsCount)
	}
}
