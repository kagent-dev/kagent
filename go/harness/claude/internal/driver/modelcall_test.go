package driver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/harness/claude/config"
	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// Totals: result.usage covers the main loop only; modelUsage also includes subagents.
func TestModelCallsFromCaptures(t *testing.T) {
	for _, tc := range []struct {
		file                          string
		wantCalls, wantPartial        int
		wantMissingIn, wantMissingOut int64
		wantTools                     []string
		wantStop                      string
		wantStops                     []string
		wantIDs                       []string // response/request ID of each call, in order
		wantParents                   []string // parent tool ID of each call, in order
		wantToolParents               []string // parent tool ID of each tool, in order
		wantReasoning                 int64
		wantErr                       string
	}{
		{file: "claude-normal.jsonl", wantCalls: 1, wantStops: []string{"stop"}, wantIDs: []string{"msg_000001/req_000001"}},
		{
			file: "claude-tools.jsonl", wantCalls: 4, wantTools: []string{"Read", "Write", "Bash"},
			wantStops: []string{"tool_call", "tool_call", "tool_call", "stop"},
			wantIDs:   []string{"msg_000001/req_000001", "msg_000002/req_000002", "msg_000003/req_000003", "msg_000004/req_000004"},
		},
		// Only an earlier subagent call's final output count never reaches stdout.
		{
			file: "claude-subagent.jsonl", wantCalls: 4, wantPartial: 1, wantMissingOut: 90,
			wantTools: []string{"Agent", "Read"}, wantReasoning: 38,
			// tool_use_result carries no message or request ID for the subagent's final call.
			wantIDs:         []string{"msg_000001/req_000001", "msg_000002/req_000002", "/", "msg_000003/req_000003"},
			wantParents:     []string{"", "toolu_000001", "toolu_000001", ""},
			wantToolParents: []string{"", "toolu_000001"},
		},
		{
			file: "claude-cancel.jsonl", wantCalls: 1, wantPartial: 1, wantStop: "error", wantIDs: []string{"msg_000001/"},
			wantErr: "claude process exited without a terminal result event",
		},
		{file: "claude-fail.jsonl", wantCalls: 1, wantPartial: 1, wantStop: "error", wantIDs: []string{"/"}},
	} {
		t.Run(tc.file, func(t *testing.T) {
			raw, err := os.ReadFile("testdata/modelcall/" + tc.file)
			if err != nil {
				t.Fatal(err)
			}
			var calls []runtime.ModelCall
			var tools, toolParents []string
			var unreported [2]int64
			open := map[string]string{}
			err = parseJSONL(t.Context(), bytes.NewReader(raw), 1<<20, map[string]modelUsage{}, nil, func(event Event) error {
				switch {
				case event.Kind == EventModelCall && event.ModelCall.UsageOnly:
					unreported[0] += event.ModelCall.InputTokens
					unreported[1] += event.ModelCall.OutputTokens
				case event.Kind == EventModelCall:
					calls = append(calls, *event.ModelCall)
				case event.ToolPhase == "started":
					tools = append(tools, event.ToolName)
					toolParents = append(toolParents, event.ParentToolID)
					open[event.ToolID] = event.ToolName
				case event.ToolPhase == "completed":
					if open[event.ToolID] != event.ToolName {
						t.Errorf("result %s %q has no matching call", event.ToolID, event.ToolName)
					}
					delete(open, event.ToolID)
				}
				return nil
			})
			if (tc.wantErr == "" && err != nil) || (tc.wantErr != "" && (err == nil || err.Error() != tc.wantErr)) {
				t.Fatalf("ParseJSONL() error = %v, want %q", err, tc.wantErr)
			}
			if !slices.Equal(tools, tc.wantTools) || len(open) != 0 {
				t.Errorf("tools = %v (unfinished %v), want %v", tools, open, tc.wantTools)
			}
			if tc.wantToolParents != nil && !slices.Equal(toolParents, tc.wantToolParents) {
				t.Errorf("tool parent tools = %q, want %q", toolParents, tc.wantToolParents)
			}
			if len(calls) != tc.wantCalls {
				t.Fatalf("calls = %d, want %d: %+v", len(calls), tc.wantCalls, calls)
			}
			var partial int
			var main, all [2]int64
			var stops, ids, parents []string
			var reasoning int64
			for _, call := range calls {
				ids = append(ids, call.ResponseID+"/"+call.RequestID)
				parents = append(parents, call.ParentToolID)
				reasoning += call.ReasoningOutputTokens
				all[0] += call.InputTokens
				all[1] += call.OutputTokens
				if call.UsagePartial {
					partial++
					// Only an interrupted call ended abnormally; a subagent snapshot just lacks final usage.
					if call.StopReason != tc.wantStop {
						t.Errorf("partial call stop reason = %q, want %q", call.StopReason, tc.wantStop)
					}
					continue
				}
				if call.ResponseModel == "" {
					t.Errorf("incomplete call %+v", call)
				}
				stops = append(stops, call.StopReason)
				// A subagent's final call has exact usage but no finish reason, and result.usage excludes it.
				if call.StopReason == "" {
					continue
				}
				main[0] += call.InputTokens
				main[1] += call.OutputTokens
			}
			if reasoning != tc.wantReasoning {
				t.Errorf("reasoning tokens = %d, want %d", reasoning, tc.wantReasoning)
			}
			if tc.wantIDs != nil && !slices.Equal(ids, tc.wantIDs) {
				t.Errorf("call IDs = %v, want %v", ids, tc.wantIDs)
			}
			if tc.wantParents != nil && !slices.Equal(parents, tc.wantParents) {
				t.Errorf("call parent tools = %v, want %v", parents, tc.wantParents)
			}
			if tc.wantStops != nil && !slices.Equal(stops, tc.wantStops) {
				t.Errorf("complete call stop reasons = %v, want %v", stops, tc.wantStops)
			}
			if partial != tc.wantPartial {
				t.Errorf("partial = %d, want %d", partial, tc.wantPartial)
			}
			usage, models, ok := claudeTotals(t, raw)
			if !ok {
				return
			}
			if main != usage {
				t.Errorf("complete call tokens = %v, want result.usage %v", main, usage)
			}
			missing := [2]int64{models[0] - all[0], models[1] - all[1]}
			if want := [2]int64{tc.wantMissingIn, tc.wantMissingOut}; missing != want {
				t.Errorf("modelUsage - calls = %v, want %v", missing, want)
			}
			if unreported != missing {
				t.Errorf("UsageOnly tokens = %v, want the missing %v", unreported, missing)
			}
		})
	}
}

func TestCapturesUseThePinnedClaudeVersion(t *testing.T) {
	files, err := filepath.Glob("testdata/modelcall/*.jsonl")
	if err != nil || len(files) == 0 {
		t.Fatalf("captures = %v, %v", files, err)
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var init struct {
			Version string `json:"claude_code_version"`
		}
		line, _, _ := bytes.Cut(raw, []byte("\n"))
		if err := json.Unmarshal(line, &init); err != nil || init.Version != config.PinnedClaudeVersion {
			t.Errorf("%s claude_code_version = %q (%v), want %s", file, init.Version, err, config.PinnedClaudeVersion)
		}
	}
}

func TestUnstreamedModelCalls(t *testing.T) {
	type call struct {
		input, output int64
		partial       bool
		start         int64
		stop          string
	}
	for _, tc := range []struct {
		name  string
		tools map[string]string
		lines []string
		want  []call
	}{
		{
			name: "subagent lines do not disturb the main call",
			lines: []string{
				`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_main","model":"claude-opus-5-5","usage":{"input_tokens":7}}}}`,
				`{"type":"stream_event","parent_tool_use_id":"toolu_1","event":{"type":"message_start","message":{"id":"msg_sub","model":"claude-opus-5-5"}}}`,
				`{"type":"stream_event","parent_tool_use_id":"toolu_1","event":{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":99}}}`,
				`{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}}`,
			},
			want: []call{{input: 7, output: 3, start: 100, stop: "stop"}},
		},
		{
			name:  "subagent calls use their latest usage",
			tools: map[string]string{"toolu_1": "Agent"},
			lines: []string{
				`{"type":"assistant","parent_tool_use_id":"toolu_1","message":{"id":"msg_a","model":"m","usage":{"input_tokens":5,"output_tokens":1}}}`,
				`{"type":"assistant","parent_tool_use_id":"toolu_1","message":{"id":"msg_a","model":"m","usage":{"input_tokens":5,"output_tokens":4}}}`,
				`{"type":"assistant","parent_tool_use_id":"toolu_1","message":{"id":"msg_b","model":"m","usage":{"input_tokens":9,"output_tokens":1}}}`,
				`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"done"}]},"tool_use_result":{"resolvedModel":"m","usage":{"input_tokens":9,"output_tokens":7}}}`,
			},
			// msg_b starts when msg_a's last snapshot arrived.
			want: []call{{input: 5, output: 4, partial: true, start: 100}, {input: 9, output: 7, start: 101}},
		},
		{
			// tool_use_result has no message ID, so an earlier call with the final call's input merges into it.
			name:  "final usage merges a held call with equal input",
			tools: map[string]string{"toolu_1": "Agent"},
			lines: []string{
				`{"type":"assistant","parent_tool_use_id":"toolu_1","message":{"id":"msg_earlier","model":"m","usage":{"input_tokens":9,"output_tokens":1}}}`,
				`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"done"}]},"tool_use_result":{"agentId":"a1","resolvedModel":"m","usage":{"input_tokens":9,"output_tokens":7}}}`,
			},
			want: []call{{input: 9, output: 7, start: 100}},
		},
		{
			// Each call starts at the subagent's previous line: its prompt, then its tool result.
			name:  "subagent calls start at the subagent's previous line",
			tools: map[string]string{"toolu_1": "Agent", "toolu_r": "Read"},
			lines: []string{
				`{"type":"user","timestamp":"1970-01-01T00:16:30Z","parent_tool_use_id":"toolu_1","message":{"content":[{"type":"text","text":"read notes"}]}}`,
				`{"type":"assistant","timestamp":"1970-01-01T00:16:40Z","parent_tool_use_id":"toolu_1","message":{"id":"msg_a","model":"m","usage":{"input_tokens":5,"output_tokens":1}}}`,
				`{"type":"user","timestamp":"1970-01-01T00:16:45Z","parent_tool_use_id":"toolu_1","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_r","content":"hello"}]}}`,
				`{"type":"assistant","timestamp":"1970-01-01T00:16:50Z","parent_tool_use_id":"toolu_1","message":{"id":"msg_b","model":"m","usage":{"input_tokens":9,"output_tokens":1}}}`,
				`{"type":"user","timestamp":"1970-01-01T00:17:00Z","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"done"}]},"tool_use_result":{"resolvedModel":"m","usage":{"input_tokens":9,"output_tokens":7}}}`,
			},
			want: []call{{input: 5, output: 1, partial: true, start: 990}, {input: 9, output: 7, start: 1005}},
		},
		{
			name:  "an unreadable timestamp falls back to the parse time",
			tools: map[string]string{"toolu_1": "Agent"},
			lines: []string{
				`{"type":"assistant","timestamp":"yesterday","parent_tool_use_id":"toolu_1","message":{"id":"msg_a","model":"m","usage":{"input_tokens":5,"output_tokens":1}}}`,
				`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"done"}]},"tool_use_result":{"resolvedModel":"m","usage":{"input_tokens":9,"output_tokens":7}}}`,
			},
			want: []call{{input: 5, output: 1, partial: true, start: 100}, {input: 9, output: 7, start: 100}},
		},
		{
			// Claude Code falls back to a non-streaming request, for example behind a gateway without streaming.
			name: "an unstreamed main-thread call is complete and starts at the previous prompt",
			lines: []string{
				`{"type":"user","message":{"content":[{"type":"text","text":"hello"}]}}`,
				`{"type":"assistant","message":{"id":"msg_1","model":"m","stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":2}}}`,
				`{"type":"assistant","message":{"id":"msg_1","model":"m","stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":2}}}`,
				`{"type":"result","subtype":"success","result":""}`,
			},
			want: []call{{input: 5, output: 2, start: 100, stop: "stop"}},
		},
		{
			name:  "non-Agent tool usage is not a model call",
			tools: map[string]string{"toolu_1": "mcp__srv__lookup"},
			lines: []string{
				`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"}]},"tool_use_result":{"usage":{"input_tokens":9,"output_tokens":7}}}`,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := time.Unix(100, 0)
			p := newParser(func() time.Time { return clock })
			maps.Copy(p.tools, tc.tools)
			var calls []runtime.ModelCall
			emit := collectCalls(&calls)
			for _, line := range tc.lines {
				if err := p.parseLine(t.Context(), []byte(line), emit); err != nil {
					t.Fatal(err)
				}
				clock = clock.Add(time.Second)
			}
			got := make([]call, 0, len(calls))
			for _, c := range calls {
				got = append(got, call{input: c.InputTokens, output: c.OutputTokens, partial: c.UsagePartial, start: c.Start.Unix(), stop: c.StopReason})
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("calls = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestRetriedModelCalls(t *testing.T) {
	type call struct {
		stop, errorType string
		retries         int
		in, out         int64
	}
	for _, tc := range []struct {
		file      string
		startLine string // the first line of the first call's first attempt
		want      []call
	}{
		// A stream cut off by an error event is retried without streaming. Both attempts' input counts.
		{file: "claude-retry-error.jsonl", startLine: `"status":"requesting"`, want: []call{{stop: "stop", retries: 1, in: 2000, out: 4}}},
		// A dropped connection ends the stream with no message_delta, then api_retry starts a new one.
		{file: "claude-retry-drop.jsonl", startLine: `"status":"requesting"`, want: []call{{stop: "stop", retries: 1, in: 2000, out: 4}}},
		// A request rejected before any stream adds no tokens.
		{file: "claude-retry-prestream.jsonl", startLine: `"status":"requesting"`, want: []call{{stop: "stop", retries: 1, in: 1000, out: 3}}},
		// After a whole tool_use block, a lost connection is a failed call, not a retry.
		{file: "claude-retry-tooldrop.jsonl", startLine: `"status":"requesting"`, want: []call{
			{stop: "error", errorType: "server_error", in: 1000, out: 1},
			{stop: "stop", in: 1000, out: 3},
		}},
	} {
		t.Run(tc.file, func(t *testing.T) {
			raw, err := os.ReadFile("testdata/modelcall/" + tc.file)
			if err != nil {
				t.Fatal(err)
			}
			clock := time.Unix(100, 0)
			p := newParser(func() time.Time { return clock })
			var calls []runtime.ModelCall
			emit := collectCalls(&calls)
			var wantStart time.Time
			for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
				if wantStart.IsZero() && strings.Contains(line, tc.startLine) {
					wantStart = clock
				}
				if err := p.parseLine(t.Context(), []byte(line), emit); err != nil {
					t.Fatal(err)
				}
				clock = clock.Add(time.Second)
			}
			if err := p.flushCalls(emit); err != nil {
				t.Fatal(err)
			}
			var got []call
			for _, c := range calls {
				got = append(got, call{c.StopReason, c.ErrorType, c.Retries, c.InputTokens, c.OutputTokens})
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("calls = %+v, want %+v", got, tc.want)
			}
			if !calls[0].Start.Equal(wantStart) {
				t.Errorf("first call starts at %v, want %v", calls[0].Start, wantStart)
			}
		})
	}
}

func TestFailedAndRetriedModelCalls(t *testing.T) {
	type call struct {
		errorType, stop string
		input           int64
		partial         bool
		retries         int
		start, end      int64
	}
	start := func(id string, input int) string {
		return fmt.Sprintf(`{"type":"stream_event","event":{"type":"message_start","message":{"id":%q,"model":"m","usage":{"input_tokens":%d}}}}`, id, input)
	}
	const (
		stop     = `{"type":"stream_event","event":{"type":"message_stop"}}`
		delta    = `{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}}`
		apiRetry = `{"type":"system","subtype":"api_retry","attempt":1,"error_status":null,"error":"unknown"}`
		init     = `{"type":"system","subtype":"init","session_id":"s1"}`
	)
	apiError := func(parent, errorType string) string {
		return `{"type":"assistant","parent_tool_use_id":"` + parent + `","is_api_error_message":true,"error":"` + errorType +
			`","message":{"id":"msg_err","model":"<synthetic>","content":[{"type":"text","text":"API Error"}],"usage":{"input_tokens":0,"output_tokens":0}}}`
	}
	for _, tc := range []struct {
		name  string
		tools map[string]string
		lines []string
		want  []call
	}{
		{
			name:  "a rejected request starts when the main thread last waited",
			lines: []string{`{"type":"system","subtype":"init","session_id":"s1"}`, apiError("", "model_not_found")},
			want:  []call{{errorType: "model_not_found", stop: "error", partial: true, start: 100, end: 101}},
		},
		{
			name: "an API error closes the open streamed call once",
			lines: []string{
				`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_1","model":"m","usage":{"input_tokens":7}}}}`,
				apiError("", "server_error"),
				`{"type":"result","subtype":"success","result":""}`,
			},
			want: []call{{errorType: "server_error", stop: "error", input: 7, partial: true, start: 100, end: 101}},
		},
		{
			name:  "a subagent API error starts at the subagent's previous line",
			tools: map[string]string{"toolu_1": "Agent"},
			lines: []string{
				`{"type":"user","parent_tool_use_id":"toolu_1","message":{"content":[{"type":"text","text":"read notes"}]}}`,
				apiError("toolu_1", "rate_limit"),
			},
			want: []call{{errorType: "rate_limit", stop: "error", partial: true, start: 100, end: 101}},
		},
		{
			name:  "an API error without a category leaves the type empty",
			lines: []string{`{"type":"system","subtype":"init","session_id":"s1"}`, apiError("", "")},
			want:  []call{{stop: "error", partial: true, start: 100, end: 101}},
		},
		{
			// The held call is complete and still emitted, after the failed one.
			name:  "a subagent API error keeps the subagent's held call",
			tools: map[string]string{"toolu_1": "Agent"},
			lines: []string{
				`{"type":"user","parent_tool_use_id":"toolu_1","message":{"content":[{"type":"text","text":"read notes"}]}}`,
				`{"type":"assistant","parent_tool_use_id":"toolu_1","message":{"id":"msg_a","model":"m","usage":{"input_tokens":5,"output_tokens":1}}}`,
				apiError("toolu_1", "rate_limit"),
				`{"type":"result","subtype":"success","result":""}`,
			},
			want: []call{
				{errorType: "rate_limit", stop: "error", partial: true, start: 101, end: 102},
				{input: 5, partial: true, start: 100, end: 101},
			},
		},
		{
			// A tool can run before the next call, but the stopped reply ended at message_stop.
			name: "a reply that stops without final usage ends at message_stop",
			lines: []string{
				start("msg_1", 7), stop, `{"type":"user","message":{"content":[{"type":"text","text":"tool ran"}]}}`,
				start("msg_2", 9), delta,
			},
			want: []call{
				{stop: "error", input: 7, partial: true, start: 100, end: 101},
				{stop: "stop", input: 9, start: 102, end: 104},
			},
		},
		{
			name:  "with no line between two calls, the next starts when the previous ends",
			lines: []string{start("msg_1", 7), delta, start("msg_2", 9), delta},
			want: []call{
				{stop: "stop", input: 7, start: 100, end: 101},
				{stop: "stop", input: 9, start: 101, end: 103},
			},
		},
		{
			name: "a call starts when Claude Code sends the request, before the first token",
			lines: []string{
				`{"type":"user","message":{"content":[{"type":"text","text":"hello"}]}}`,
				`{"type":"system","subtype":"status","status":"requesting"}`,
				start("msg_1", 7), delta,
			},
			want: []call{{stop: "stop", input: 7, start: 101, end: 103}},
		},
		{
			// A tool starts before the final usage, which still sets the end of a normal reply.
			name:  "a normal reply with a tool ends at message_delta",
			tools: map[string]string{"toolu_1": "Bash"},
			lines: []string{
				start("msg_1", 7),
				`{"type":"assistant","message":{"id":"msg_1","model":"m","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{}}]}}`,
				delta,
			},
			want: []call{{stop: "stop", input: 7, start: 100, end: 102}},
		},
		{
			// The tool runs while the connection hangs, so the failed reply ends when its tool starts.
			name:  "a reply cut off after a whole tool request ends at the tool start",
			tools: map[string]string{"toolu_1": "Bash"},
			lines: []string{
				start("msg_1", 7),
				`{"type":"assistant","message":{"id":"msg_1","model":"m","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{}}]}}`,
				`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"}]}}`,
				apiError("", "server_error"),
			},
			want: []call{{errorType: "server_error", stop: "error", input: 7, partial: true, start: 100, end: 101}},
		},
		{
			name:  "an API error after a stopped reply keeps the reply's end",
			lines: []string{start("msg_1", 7), stop, apiError("", "server_error")},
			want:  []call{{errorType: "server_error", stop: "error", input: 7, partial: true, start: 100, end: 101}},
		},
		{
			name:  "an API error after a tool request is a partial tool call",
			tools: map[string]string{"call-1": "mcp__protected__write"},
			lines: []string{
				start("msg_1", 7),
				`{"type":"assistant","message":{"id":"msg_1","model":"m","content":[{"type":"tool_use","id":"call-1","name":"mcp__protected__write","input":{}}]}}`,
				apiError("", "server_error"),
			},
			want: []call{{stop: "tool_call", input: 7, partial: true, start: 100, end: 101}},
		},
		{
			name:  "retries that end in a failure are one failed call",
			lines: []string{init, apiRetry, apiRetry, apiError("", "overloaded_error")},
			want:  []call{{errorType: "overloaded_error", stop: "error", partial: true, retries: 2, start: 100, end: 103}},
		},
		{
			name:  "a call retried twice starts at its first attempt",
			lines: []string{start("msg_1", 7), stop, apiRetry, start("msg_2", 9), stop, apiRetry, start("msg_3", 11), delta},
			want:  []call{{stop: "stop", input: 27, retries: 2, start: 100, end: 107}},
		},
		{
			name:  "a retry left at the end of the stream is a failed call",
			lines: []string{start("msg_1", 7), stop, apiRetry},
			want:  []call{{stop: "error", input: 7, partial: true, retries: 1, start: 100, end: 103}},
		},
		{
			// A mid-stream overload is retried with a new stream and no api_retry line.
			name:  "a new stream after an unfinished attempt is a retry",
			lines: []string{start("msg_1", 7), stop, start("msg_2", 9), delta},
			want:  []call{{stop: "stop", input: 16, retries: 1, start: 100, end: 103}},
		},
		{
			name:  "a retried call cut off by a new stream adds that retry",
			lines: []string{init, apiRetry, start("msg_1", 7), start("msg_2", 9), delta},
			want:  []call{{stop: "stop", input: 16, retries: 2, start: 100, end: 104}},
		},
		{
			// A reply sent without streaming can arrive as one line per content block.
			name: "repeated lines of an unstreamed reply keep its retries",
			lines: []string{
				start("msg_1", 1000), stop, apiRetry,
				`{"type":"assistant","message":{"id":"msg_u","model":"m","stop_reason":"end_turn","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":1000,"output_tokens":3}}}`,
				`{"type":"assistant","message":{"id":"msg_u","model":"m","stop_reason":"end_turn","content":[{"type":"text","text":"hi there"}],"usage":{"input_tokens":1000,"output_tokens":3}}}`,
			},
			want: []call{{stop: "stop", input: 2000, retries: 1, start: 100, end: 104}},
		},
		{
			name: "a held unstreamed reply keeps its retries when the next reply replaces it",
			lines: []string{
				start("msg_1", 1000), stop, apiRetry,
				`{"type":"assistant","message":{"id":"msg_u1","model":"m","stop_reason":"tool_use","usage":{"input_tokens":1000,"output_tokens":3}}}`,
				`{"type":"assistant","message":{"id":"msg_u2","model":"m","stop_reason":"end_turn","usage":{"input_tokens":1200,"output_tokens":2}}}`,
			},
			want: []call{
				{stop: "tool_call", input: 2000, retries: 1, start: 100, end: 103},
				{stop: "stop", input: 1200, start: 103, end: 104},
			},
		},
		{
			name:  "a synthetic message without the API error flag is not a call",
			lines: []string{`{"type":"assistant","message":{"id":"msg_x","model":"<synthetic>","content":[{"type":"text","text":"No response requested."}]}}`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := time.Unix(100, 0)
			p := newParser(func() time.Time { return clock })
			maps.Copy(p.tools, tc.tools)
			var calls []runtime.ModelCall
			emit := collectCalls(&calls)
			for _, line := range tc.lines {
				if err := p.parseLine(t.Context(), []byte(line), emit); err != nil {
					t.Fatal(err)
				}
				clock = clock.Add(time.Second)
			}
			if err := p.flushCalls(emit); err != nil {
				t.Fatal(err)
			}
			var got []call
			for _, c := range calls {
				got = append(got, call{errorType: c.ErrorType, stop: c.StopReason, input: c.InputTokens, partial: c.UsagePartial,
					retries: c.Retries, start: c.Start.Unix(), end: c.End.Unix()})
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("calls = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestMalformedSubagentUsageKeepsTheHeldCall(t *testing.T) {
	var logs bytes.Buffer
	ctx := logging.IntoContext(t.Context(), slog.New(slog.NewTextHandler(&logs, nil)))
	p := newParser(time.Now)
	p.tools["toolu_1"] = "Agent"
	var calls []runtime.ModelCall
	emit := collectCalls(&calls)
	for _, line := range []string{
		`{"type":"assistant","parent_tool_use_id":"toolu_1","message":{"id":"msg_a","model":"m","usage":{"input_tokens":5,"output_tokens":1}}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"done"}]},"tool_use_result":{"usage":{"input_tokens":"many"}}}`,
	} {
		if err := p.parseLine(ctx, []byte(line), emit); err != nil {
			t.Fatal(err)
		}
	}
	if len(calls) != 1 || calls[0].InputTokens != 5 {
		t.Fatalf("calls = %+v, want only the held call", calls)
	}
	if !strings.Contains(logs.String(), "level=WARN") {
		t.Errorf("logs = %q, want a warning", logs.String())
	}
}

// claudeTotals returns [input incl. cache, output] from result.usage and summed modelUsage.
func claudeTotals(t *testing.T, raw []byte) (usage, models [2]int64, ok bool) {
	t.Helper()
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(nil, 1<<20)
	for scanner.Scan() {
		var result struct {
			Type       string
			Usage      claudeUsage
			ModelUsage map[string]struct {
				InputTokens, OutputTokens, CacheReadInputTokens, CacheCreationInputTokens int64
			}
		}
		if err := json.Unmarshal(scanner.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Type != "result" {
			continue
		}
		u := result.Usage
		usage = [2]int64{u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens, u.OutputTokens}
		for _, m := range result.ModelUsage {
			models[0] += m.InputTokens + m.CacheReadInputTokens + m.CacheCreationInputTokens
			models[1] += m.OutputTokens
		}
		return usage, models, true
	}
	return usage, models, false
}

func collectCalls(calls *[]runtime.ModelCall) func(Event) error {
	return func(event Event) error {
		if event.Kind == EventModelCall {
			*calls = append(*calls, *event.ModelCall)
		}
		return nil
	}
}

func TestParseFailureFlushesTheOpenCall(t *testing.T) {
	input := `{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_main","model":"claude-opus-5-5","usage":{"input_tokens":7}}}}` + "\n{not json\n"
	var calls []runtime.ModelCall
	if err := parseJSONL(t.Context(), strings.NewReader(input), 4096, map[string]modelUsage{}, nil, collectCalls(&calls)); err == nil || !strings.Contains(err.Error(), "decode Claude event") {
		t.Fatalf("ParseJSONL() error = %v, want the decode error", err)
	}
	if len(calls) != 1 || !calls[0].UsagePartial || calls[0].InputTokens != 7 {
		t.Fatalf("calls = %+v, want the started call flushed as partial", calls)
	}
}

func TestUnreportedUsageGoesToTheCounters(t *testing.T) {
	type usage struct {
		model                    string
		input, cacheRead, output int64
	}
	// One main-thread call of model m reports 10 input tokens, 2 of them cache reads, and 5 output tokens.
	oneCall := []string{
		`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_1","model":"m","usage":{"input_tokens":8,"cache_read_input_tokens":2}}}}`,
		`{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}}`,
	}
	for _, tc := range []struct {
		name   string
		base   map[string]modelUsage
		call   []string // the calls before the result, or nil for oneCall
		totals string
		want   []usage
	}{
		{
			name:   "a new session adds what no call reported",
			base:   map[string]modelUsage{},
			totals: `{"m":{"inputTokens":9,"cacheReadInputTokens":2,"outputTokens":20}}`,
			want:   []usage{{"m", 1, 0, 15}},
		},
		{
			name:   "a resumed session leaves out the earlier processes",
			base:   map[string]modelUsage{"m": {InputTokens: 100, OutputTokens: 50}},
			totals: `{"m":{"inputTokens":108,"cacheReadInputTokens":2,"outputTokens":70}}`,
			want:   []usage{{"m", 0, 0, 15}},
		},
		{
			name:   "a call Claude Code does not count adds nothing",
			base:   map[string]modelUsage{},
			totals: `{"m":{"inputTokens":0,"outputTokens":0}}`,
		},
		{
			name:   "each model adds its own tokens",
			base:   map[string]modelUsage{},
			totals: `{"m":{"inputTokens":8,"cacheReadInputTokens":2,"outputTokens":5},"small":{"inputTokens":30,"cacheReadInputTokens":4,"outputTokens":3}}`,
			want:   []usage{{"small", 34, 4, 3}},
		},
		{
			name: "a dated response model counts under the model it was requested as",
			base: map[string]modelUsage{},
			call: []string{
				`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_1","model":"m-20250929","usage":{"input_tokens":8,"cache_read_input_tokens":2}}}}`,
				`{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}}`,
			},
			totals: `{"m":{"inputTokens":8,"cacheReadInputTokens":2,"outputTokens":20}}`,
			want:   []usage{{"m", 0, 0, 15}},
		},
		{
			name:   "totals that name other models than the calls add nothing",
			base:   map[string]modelUsage{},
			totals: `{"us.anthropic.m-v1:0":{"inputTokens":9,"outputTokens":20}}`,
		},
		{
			name:   "unknown session usage adds nothing",
			totals: `{"m":{"inputTokens":9,"outputTokens":20}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := tc.call
			if calls == nil {
				calls = oneCall
			}
			lines := append(slices.Clone(calls), `{"type":"result","subtype":"success","result":"","modelUsage":`+tc.totals+`}`)
			var got []usage
			err := parseJSONL(t.Context(), strings.NewReader(strings.Join(lines, "\n")), 4096, tc.base, nil, func(event Event) error {
				if c := event.ModelCall; event.Kind == EventModelCall && c.UsageOnly {
					got = append(got, usage{c.RequestModel, c.InputTokens, c.CacheReadTokens, c.OutputTokens})
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("usage-only events = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSessionUsage(t *testing.T) {
	const session = "11111111-1111-4111-8111-111111111111"
	for _, tc := range []struct {
		name    string
		lines   []string // the session file, or nil for none
		id      string
		want    map[string]modelUsage
		wantErr bool
	}{
		{name: "a new session starts at zero", want: map[string]modelUsage{}},
		{
			name: "the last saved totals win",
			id:   session,
			lines: []string{
				`{"type":"user","message":{"content":"hi"}}`,
				`{"type":"cost-state","modelUsage":{"m":{"inputTokens":1,"outputTokens":2}}}`,
				`{"type":"cost-state","modelUsage":{"m":{"inputTokens":3,"outputTokens":4}}}`,
			},
			want: map[string]modelUsage{"m": {InputTokens: 3, OutputTokens: 4}},
		},
		{
			name:  "a session with no saved totals starts at zero",
			id:    session,
			lines: []string{`{"type":"user","message":{"content":"hi"}}`},
			want:  map[string]modelUsage{},
		},
		{name: "a missing session file is unknown", id: session, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.lines != nil {
				project := filepath.Join(dir, "projects", "-work-sandbox")
				if err := os.MkdirAll(project, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(project, tc.id+".jsonl"), []byte(strings.Join(tc.lines, "\n")+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := sessionUsage([]string{"HOME=/data", config.ClaudeConfigDirEnvName + "=" + dir}, tc.id)
			if (err != nil) != tc.wantErr || (got == nil) != tc.wantErr {
				t.Fatalf("sessionUsage() = %v, %v, want error %t", got, err, tc.wantErr)
			}
			if !maps.Equal(got, tc.want) {
				t.Errorf("sessionUsage() = %v, want %v", got, tc.want)
			}
		})
	}
}
