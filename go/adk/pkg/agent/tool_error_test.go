package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/toolconfirmation"
)

// toolErrorContext is the slice of agent.Context the on-tool-error callback
// reads: the fields of its log line, and the turn's confirmation.
type toolErrorContext struct {
	adkagent.StrictContextMock
	confirmation *toolconfirmation.ToolConfirmation
}

func (toolErrorContext) FunctionCallID() string { return "call-1" }
func (toolErrorContext) SessionID() string      { return "session-1" }
func (toolErrorContext) InvocationID() string   { return "invocation-1" }

func (c *toolErrorContext) ToolConfirmation() *toolconfirmation.ToolConfirmation {
	return c.confirmation
}

type namedTool struct{ name string }

func (t namedTool) Name() string      { return t.name }
func (namedTool) Description() string { return "" }
func (namedTool) IsLongRunning() bool { return false }

func newToolErrorContext(confirmation *toolconfirmation.ToolConfirmation) *toolErrorContext {
	return &toolErrorContext{
		StrictContextMock: adkagent.NewStrictContextMock(context.Background()),
		confirmation:      confirmation,
	}
}

func rejectedWith(payload map[string]any) *toolconfirmation.ToolConfirmation {
	return &toolconfirmation.ToolConfirmation{Confirmed: false, Payload: payload}
}

func TestOnToolErrorCallback_FlagsAFailedCall(t *testing.T) {
	callback := makeOnToolErrorCallback(slog.New(slog.DiscardHandler))
	// The error adk-go's MCP toolset returns for a CallToolResult with IsError set.
	message := "Tool execution failed. Details: bad_data: invalid parameter \"query\": parse error"
	err := errors.New(message)

	response, cbErr := callback(newToolErrorContext(nil), namedTool{name: "call_tool"}, nil, err)

	require.NoError(t, cbErr)
	require.Equal(t, map[string]any{"error": message, "isError": true}, response)
}

func TestOnToolErrorCallback_TellsTheModelWhyAPersonRejectedACall(t *testing.T) {
	callback := makeOnToolErrorCallback(slog.New(slog.DiscardHandler))
	ctx := newToolErrorContext(rejectedWith(map[string]any{"rejection_reason": "What does this tool do?"}))
	err := fmt.Errorf("error tool %q %w", "call_tool", tool.ErrConfirmationRejected)

	response, cbErr := callback(ctx, namedTool{name: "call_tool"}, nil, err)

	require.NoError(t, cbErr)
	require.Equal(t, map[string]any{
		"error":   "Tool call was rejected by user. Reason: What does this tool do?",
		"isError": true,
	}, response)
}

func TestOnToolErrorCallback_LeavesAConfirmationRequestToTheFlow(t *testing.T) {
	callback := makeOnToolErrorCallback(slog.New(slog.DiscardHandler))
	err := fmt.Errorf("error tool %q %w", "call_tool", tool.ErrConfirmationRequired)

	response, cbErr := callback(newToolErrorContext(nil), namedTool{name: "call_tool"}, nil, err)

	require.NoError(t, cbErr)
	require.Nil(t, response, "a confirmation request is a pause, not a failure: adk-go builds the plain response")
}

func TestToolErrorResponse(t *testing.T) {
	require.Nil(t, toolErrorResponse(nil, nil))

	err := errors.New("connection refused")
	require.Equal(t, map[string]any{"error": "connection refused", "isError": true}, toolErrorResponse(nil, err))
}

func TestRejectionMessage(t *testing.T) {
	for _, tc := range []struct {
		name         string
		confirmation *toolconfirmation.ToolConfirmation
		want         string
	}{{
		name:         "the reason the person gave",
		confirmation: rejectedWith(map[string]any{"rejection_reason": "not production"}),
		want:         "Tool call was rejected by user. Reason: not production",
	}, {
		name:         "no reason given",
		confirmation: rejectedWith(nil),
		want:         "Tool call was rejected by user.",
	}, {
		name:         "a blank reason is no reason",
		confirmation: rejectedWith(map[string]any{"rejection_reason": "   "}),
		want:         "Tool call was rejected by user.",
	}, {
		// A rejection propagated from a child agent carries that agent's own
		// decisions, not a reason of its own.
		name:         "a nested confirmation",
		confirmation: rejectedWith(map[string]any{"subagent_name": "child", "task_id": "child-task"}),
		want:         "Tool call was rejected by user.",
	}, {
		name:         "no confirmation at all",
		confirmation: nil,
		want:         "Tool call was rejected by user.",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, rejectionMessage(tc.confirmation))
		})
	}
}
