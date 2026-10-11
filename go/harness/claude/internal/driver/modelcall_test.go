package driver

import (
	"bufio"
	"bytes"
	"encoding/json"
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
			open := map[string]string{}
			err = ParseJSONL(t.Context(), bytes.NewReader(raw), 1<<20, func(event Event) error {
				switch {
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

func TestFailedModelCalls(t *testing.T) {
	type call struct {
		errorType, stop string
		input           int64
		partial         bool
		start, end      int64
	}
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
			var got []call
			for _, c := range calls {
				got = append(got, call{errorType: c.ErrorType, stop: c.StopReason, input: c.InputTokens, partial: c.UsagePartial,
					start: c.Start.Unix(), end: c.End.Unix()})
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
	if err := ParseJSONL(t.Context(), strings.NewReader(input), 4096, collectCalls(&calls)); err == nil || !strings.Contains(err.Error(), "decode Claude event") {
		t.Fatalf("ParseJSONL() error = %v, want the decode error", err)
	}
	if len(calls) != 1 || !calls[0].UsagePartial || calls[0].InputTokens != 7 {
		t.Fatalf("calls = %+v, want the started call flushed as partial", calls)
	}
}
