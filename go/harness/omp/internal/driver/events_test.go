package driver

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/harness/runtime"
)

type recordingSink struct {
	text    strings.Builder
	calls   []runtime.ToolCall
	results []runtime.ToolResult
}

func (*recordingSink) SessionStarted(runtime.SessionStarted) error { return nil }
func (*recordingSink) Notice(runtime.Notice) error                 { return nil }
func (s *recordingSink) TextDelta(event runtime.TextDelta) error {
	s.text.WriteString(event.Text)
	return nil
}
func (s *recordingSink) ToolCall(event runtime.ToolCall) error {
	s.calls = append(s.calls, event)
	return nil
}
func (s *recordingSink) ToolResult(event runtime.ToolResult) error {
	s.results = append(s.results, event)
	return nil
}

func parseFrame(t *testing.T, line string) frame {
	t.Helper()
	var f frame
	require.NoError(t, json.Unmarshal([]byte(line), &f))
	f.Raw = json.RawMessage(line)
	return f
}

// run feeds lines to a tracker of prompt "7" and returns after which line it
// reported the prompt over (-1: never) and the replies it sent.
func run(t *testing.T, sink runtime.EventSink, lines ...string) (*promptTracker, int, []any) {
	t.Helper()
	tracker := &promptTracker{promptID: "7"}
	var replies []any
	reply := func(message any) error {
		replies = append(replies, message)
		return nil
	}
	for index, line := range lines {
		done, err := tracker.translate(parseFrame(t, line), sink, reply)
		require.NoError(t, err)
		if done {
			return tracker, index, replies
		}
	}
	return tracker, -1, replies
}

func TestTranslateCompletedPrompt(t *testing.T) {
	sink := &recordingSink{}
	tracker, over, replies := run(t, sink,
		`{"type":"response","id":"7","command":"prompt","success":true,"data":{"agentInvoked":true}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","delta":"hmm"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"hel"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"lo"}}`,
		`{"type":"tool_execution_start","toolCallId":"c1","toolName":"mcp__muster_list","args":{"q":"x"}}`,
		`{"type":"tool_execution_end","toolCallId":"c1","toolName":"mcp__muster_list","result":{"ok":true},"isError":false}`,
		`{"type":"prompt_result","id":"6","status":"failed"}`,
		`{"type":"prompt_result","id":"7","status":"completed","sessionSettled":false}`,
		`{"type":"session_settled"}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"late"}}`,
	)
	assert.Equal(t, 8, over, "the prompt is over once its result is in and the session settled")
	assert.Empty(t, replies)
	assert.Equal(t, "hello", sink.text.String(), "only text deltas reach the sink")
	assert.Equal(t, []runtime.ToolCall{{ID: "c1", Name: "mcp__muster_list", Arguments: map[string]any{"q": "x"}}}, sink.calls)
	assert.Equal(t, []runtime.ToolResult{{ID: "c1", Name: "mcp__muster_list", Result: map[string]any{"ok": true}}}, sink.results)
	assert.Nil(t, tracker.failure())
}

func TestTranslateSettledResult(t *testing.T) {
	_, over, _ := run(t, &recordingSink{}, `{"type":"prompt_result","id":"7","status":"completed","sessionSettled":true}`)
	assert.Equal(t, 0, over)
}

func TestTranslateSettledBeforeResult(t *testing.T) {
	_, over, _ := run(t, &recordingSink{},
		`{"type":"session_settled"}`,
		`{"type":"prompt_result","id":"7","status":"completed","sessionSettled":false}`,
	)
	assert.Equal(t, -1, over, "a settle before the prompt's result does not end the prompt")
}

func TestTranslateLocalCommand(t *testing.T) {
	tracker, over, _ := run(t, &recordingSink{},
		`{"type":"response","id":"7","command":"prompt","success":true,"data":{"agentInvoked":false}}`)
	assert.Equal(t, 0, over, "a prompt a builtin command finished locally ends with its response")
	assert.Nil(t, tracker.failure())
}

func TestTranslateCancelsDialogs(t *testing.T) {
	_, over, replies := run(t, &recordingSink{},
		`{"type":"extension_ui_request","id":"d1","method":"confirm"}`,
		`{"type":"extension_ui_request","id":"n1","method":"notify"}`,
	)
	assert.Equal(t, -1, over)
	assert.Equal(t, []any{uiCancel{Type: "extension_ui_response", ID: "d1", Cancelled: true}}, replies,
		"a dialog resolves to its default, a notification needs no answer")
}

func TestFailure(t *testing.T) {
	for name, test := range map[string]struct {
		line string
		want string
	}{
		"error message": {`{"type":"prompt_result","id":"7","status":"failed","sessionSettled":true,"error":{"message":"model refused"}}`, "model refused"},
		"status only":   {`{"type":"prompt_result","id":"7","status":"aborted","sessionSettled":true}`, "omp ended the turn with status aborted"},
	} {
		t.Run(name, func(t *testing.T) {
			tracker, over, _ := run(t, &recordingSink{}, test.line)
			require.Equal(t, 0, over)
			require.NotNil(t, tracker.failure())
			assert.Equal(t, test.want, tracker.failure().Message)
		})
	}
}

func TestTranslateRejectsMalformedEvent(t *testing.T) {
	tracker := &promptTracker{promptID: "7"}
	f := frame{Type: frameToolStart, Raw: json.RawMessage(`{"toolCallId":1}`)}
	_, err := tracker.translate(f, &recordingSink{}, nil)
	assert.ErrorContains(t, err, "decode omp tool start")
}

func TestTurnUsage(t *testing.T) {
	stats := func(messages, input, output, cacheRead, cacheWrite int, cost float64) sessionStats {
		var s sessionStats
		s.AssistantMessages, s.Cost = messages, cost
		s.Tokens.Input, s.Tokens.Output, s.Tokens.CacheRead, s.Tokens.CacheWrite = input, output, cacheRead, cacheWrite
		return s
	}
	usage := turnUsage(stats(2, 300, 40, 100, 50, 0.25), stats(5, 400, 47, 120, 80, 0.75))
	assert.Equal(t, &runtime.Usage{
		TotalCostUSD: 0.5, NumTurns: 3,
		InputTokens: 100 + 20 + 30, CachedTokens: 20, OutputTokens: 7,
	}, usage, "the turn's usage is the difference, every prompt token counted as input")
}
