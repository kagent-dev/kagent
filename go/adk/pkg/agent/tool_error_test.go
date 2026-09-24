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
)

// toolErrorContext is the slice of agent.Context the on-tool-error callback
// reads for its log line.
type toolErrorContext struct {
	adkagent.StrictContextMock
}

func (toolErrorContext) FunctionCallID() string { return "call-1" }
func (toolErrorContext) SessionID() string      { return "session-1" }
func (toolErrorContext) InvocationID() string   { return "invocation-1" }

type namedTool struct{ name string }

func (t namedTool) Name() string      { return t.name }
func (namedTool) Description() string { return "" }
func (namedTool) IsLongRunning() bool { return false }

func newToolErrorContext() *toolErrorContext {
	return &toolErrorContext{StrictContextMock: adkagent.NewStrictContextMock(context.Background())}
}

func TestOnToolErrorCallback_FlagsAFailedCall(t *testing.T) {
	callback := makeOnToolErrorCallback(slog.New(slog.DiscardHandler))
	// The error adk-go's MCP toolset returns for a CallToolResult with IsError set.
	message := "Tool execution failed. Details: bad_data: invalid parameter \"query\": parse error"
	err := errors.New(message)

	response, cbErr := callback(newToolErrorContext(), namedTool{name: "call_tool"}, nil, err)

	require.NoError(t, cbErr)
	require.Equal(t, map[string]any{"error": message, "isError": true}, response)
}

func TestOnToolErrorCallback_LeavesAConfirmationRequestToTheFlow(t *testing.T) {
	callback := makeOnToolErrorCallback(slog.New(slog.DiscardHandler))
	err := fmt.Errorf("error tool %q %w", "call_tool", tool.ErrConfirmationRequired)

	response, cbErr := callback(newToolErrorContext(), namedTool{name: "call_tool"}, nil, err)

	require.NoError(t, cbErr)
	require.Nil(t, response, "a confirmation request is a pause, not a failure: adk-go builds the plain response")
}

func TestToolErrorResponse(t *testing.T) {
	rejected := fmt.Errorf("error tool %q %w", "call_tool", tool.ErrConfirmationRejected)
	require.Equal(t, map[string]any{"error": rejected.Error(), "isError": true}, toolErrorResponse(rejected),
		"a rejected call did not run, so it is a failed call")
	require.Nil(t, toolErrorResponse(nil))
}
