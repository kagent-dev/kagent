package driver

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

const pinnedClaudeVersion = "2.1.260"

func TestParseJSONLStreamingAndDeduplication(t *testing.T) {
	b, err := os.ReadFile("../../testdata/stream-success.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	for _, reader := range []io.Reader{bytes.NewReader(b), &fragmentReader{data: b, size: 3}} {
		var events []Event
		if err := ParseJSONL(reader, 4096, func(event Event) error {
			events = append(events, event)
			return nil
		}); err != nil {
			t.Fatalf("ParseJSONL() error = %v", err)
		}
		var text strings.Builder
		for _, event := range events {
			if event.Kind == EventTextDelta {
				text.WriteString(event.Text)
			}
		}
		if text.String() != "hello" {
			t.Errorf("streamed text = %q, want hello", text.String())
		}
		if events[0].Kind != EventSessionStarted || events[len(events)-1].Kind != EventCompleted {
			t.Errorf("event boundaries = %q..%q", events[0].Kind, events[len(events)-1].Kind)
		}
	}
}

func TestParseJSONLBedrockTextAfterThinkingIsNotDuplicated(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_bdrk"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"..."}}}`,
		`{"type":"assistant","message":{"id":"msg_bdrk","content":[{"type":"thinking","thinking":"..."}]}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":0}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"alpha"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":" beta gamma"}}}`,
		`{"type":"assistant","message":{"id":"msg_bdrk","content":[{"type":"text","text":"alpha beta gamma"}]}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":1}}`,
		`{"type":"stream_event","event":{"type":"message_stop"}}`,
		`{"type":"result","subtype":"success","result":"alpha beta gamma"}`,
	}, "\n") + "\n"

	var text strings.Builder
	if err := ParseJSONL(strings.NewReader(input), 4096, func(event Event) error {
		if event.Kind == EventTextDelta {
			text.WriteString(event.Text)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if text.String() != "alpha beta gamma" {
		t.Fatalf("streamed text = %q, want alpha beta gamma", text.String())
	}
}

func TestParseJSONLTerminalFailure(t *testing.T) {
	b, err := os.ReadFile("../../testdata/stream-error.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var last Event
	if err := ParseJSONL(bytes.NewReader(b), 4096, func(event Event) error { last = event; return nil }); err != nil {
		t.Fatal(err)
	}
	if last.Kind != EventFailed || last.Category != "error_max_budget_usd" {
		t.Fatalf("last event = %#v", last)
	}
}

func TestParseJSONLBuiltInToolLifecycle(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"11111111-1111-4111-8111-111111111111"}`,
		`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_tool"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tool-1","name":"Read","input":{}}}}`,
		`{"type":"assistant","message":{"id":"msg_tool","content":[{"type":"tool_use","id":"tool-1","name":"Read","input":{"file_path":"/data/workspace/README.md"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool-1","content":"file contents","is_error":false}]}}`,
		`{"type":"assistant","message":{"id":"msg_edit","content":[{"type":"tool_use","id":"tool-2","name":"Edit","input":{"file_path":"/data/workspace/missing.md"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool-2","content":[{"type":"text","text":"file not found"}],"is_error":true}]}}`,
		`{"type":"assistant","message":{"id":"msg_done","content":[{"type":"text","text":"done"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"done"}`,
	}, "\n") + "\n"
	var events []Event
	if err := ParseJSONL(strings.NewReader(input), 4096, func(event Event) error {
		events = append(events, event)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var tools []Event
	for _, event := range events {
		if event.Kind == EventToolActivity {
			tools = append(tools, event)
		}
	}
	if len(tools) != 4 {
		t.Fatalf("tool events = %#v, want two calls and results", tools)
	}
	if tools[0].ToolPhase != "started" || tools[0].ToolID != "tool-1" || tools[0].ToolName != "Read" || tools[0].Metadata["file_path"] != "/data/workspace/README.md" {
		t.Fatalf("tool call = %#v", tools[0])
	}
	if tools[1].ToolPhase != "completed" || tools[1].ToolID != "tool-1" || tools[1].ToolName != "Read" || tools[1].ToolResult != "file contents" || tools[1].ToolError {
		t.Fatalf("tool result = %#v", tools[1])
	}
	if tools[2].ToolPhase != "started" || tools[2].ToolID != "tool-2" || tools[2].ToolName != "Edit" {
		t.Fatalf("second tool call = %#v", tools[2])
	}
	if tools[3].ToolPhase != "completed" || tools[3].ToolID != "tool-2" || tools[3].ToolName != "Edit" || !tools[3].ToolError {
		t.Fatalf("failed tool result = %#v", tools[3])
	}
}

func TestParseJSONLIgnoresSubagentTaskNotificationResult(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"11111111-1111-4111-8111-111111111111"}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"parent done"}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"child done","origin":{"kind":"task-notification"}}`,
	}, "\n") + "\n"
	var terminal []Event
	if err := ParseJSONL(strings.NewReader(input), 4096, func(event Event) error {
		if event.Kind == EventCompleted || event.Kind == EventFailed {
			terminal = append(terminal, event)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(terminal) != 1 || terminal[0].Result != "parent done" {
		t.Fatalf("terminal events = %#v, want only the parent result", terminal)
	}
}

func TestParseJSONLRejectsInvalidToolLifecycle(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  string
	}{
		{
			name: "unknown result",
			lines: []string{
				`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"missing","content":"result"}]}}`,
			},
			want: "unknown tool_use id",
		},
		{
			name: "duplicate result",
			lines: []string{
				`{"type":"assistant","message":{"id":"msg","content":[{"type":"tool_use","id":"tool-1","name":"Edit","input":{}}]}}`,
				`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool-1","content":"ok"}]}}`,
				`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool-1","content":"again"}]}}`,
			},
			want: "more than once",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := strings.Join(tt.lines, "\n") + "\n"
			err := ParseJSONL(strings.NewReader(input), 4096, func(Event) error { return nil })
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseJSONL() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestParseJSONLErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
		max   int
		want  string
	}{
		{name: "malformed", input: "{nope}\n", max: 100, want: "decode Claude event"},
		{name: "oversized", input: strings.Repeat("x", 101) + "\n", max: 100, want: "exceeds 100 bytes"},
		{name: "missing terminal", input: `{"type":"system","subtype":"init","session_id":"11111111-1111-4111-8111-111111111111"}` + "\n", max: 1024, want: "without a terminal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ParseJSONL(strings.NewReader(tt.input), tt.max, func(Event) error { return nil })
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseJSONL() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestParseJSONLPropagatesEmitterError(t *testing.T) {
	want := errors.New("stop")
	input := `{"type":"result","subtype":"success"}` + "\n"
	if err := ParseJSONL(strings.NewReader(input), 1024, func(Event) error { return want }); !errors.Is(err, want) {
		t.Fatalf("ParseJSONL() error = %v, want %v", err, want)
	}
}

type fragmentReader struct {
	data []byte
	size int
}

func (r *fragmentReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := min(r.size, len(r.data), len(p))
	copy(p, r.data[:n])
	r.data = r.data[n:]
	return n, nil
}
