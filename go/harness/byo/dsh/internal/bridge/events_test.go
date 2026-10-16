package bridge

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/kagent-dev/kagent/go/harness/runtime"
)

type recordingSink struct {
	id        string
	texts     []string
	calls     []runtime.ToolCall
	results   []runtime.ToolResult
	callReady chan struct{}
}

func (s *recordingSink) SessionStarted(v runtime.SessionStarted) error {
	s.id = v.ContinuationID
	return nil
}
func (s *recordingSink) TextDelta(v runtime.TextDelta) error {
	s.texts = append(s.texts, v.Text)
	return nil
}
func (s *recordingSink) ToolCall(v runtime.ToolCall) error {
	s.calls = append(s.calls, v)
	if s.callReady != nil {
		close(s.callReady)
		s.callReady = nil
	}
	return nil
}
func (s *recordingSink) ToolResult(v runtime.ToolResult) error {
	s.results = append(s.results, v)
	return nil
}
func (s *recordingSink) ModelCall(runtime.ModelCall) {}

func TestToolArgumentsPreserveNonObjectInput(t *testing.T) {
	for _, raw := range []string{`[]`, `42`, `true`, `null`, `"invalid"`, `"{bad json"`} {
		t.Run(raw, func(t *testing.T) {
			e := events{sessionID: "session", tools: make(map[string]tool)}
			sink := &recordingSink{}
			input := json.RawMessage(`{"sessionId":"session","update":{"sessionUpdate":"tool_call","toolCallId":"call","title":"bash","rawInput":` + raw + `}}`)
			require.NoError(t, e.apply(input, sink))
			encoded, err := json.Marshal(sink.calls[0].Arguments)
			require.NoError(t, err)
			require.JSONEq(t, `{"rawInput":`+raw+`}`, string(encoded))
			// A2A gRPC converts data parts with structpb; JSON marshaling alone
			// would also accept Go values that the transport cannot represent.
			_, err = structpb.NewStruct(sink.calls[0].Arguments)
			require.NoError(t, err)
			require.NoError(t, e.apply(json.RawMessage(`{"sessionId":"session","update":{"sessionUpdate":"tool_call_update","toolCallId":"call","status":"failed","content":[{"type":"content","content":{"type":"text","text":"invalid tool input"}}]}}`), sink))
			require.True(t, sink.results[0].IsError)
			require.Equal(t, "invalid tool input", sink.results[0].Result)
		})
	}
}
