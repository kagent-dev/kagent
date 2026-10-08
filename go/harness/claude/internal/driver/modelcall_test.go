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
		wantErr                       string
	}{
		{file: "claude-normal.jsonl", wantCalls: 1},
		{file: "claude-tools.jsonl", wantCalls: 4, wantTools: []string{"Read", "Write", "Bash"}},
		// Only an earlier subagent call's final output count never reaches stdout.
		{
			file: "claude-subagent.jsonl", wantCalls: 4, wantPartial: 1, wantMissingOut: 90,
			wantTools: []string{"Agent", "Read"},
		},
		{
			file: "claude-cancel.jsonl", wantCalls: 1, wantPartial: 1, wantStop: "error",
			wantErr: "claude process exited without a terminal result event",
		},
		{file: "claude-fail.jsonl"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			raw, err := os.ReadFile("testdata/modelcall/" + tc.file)
			if err != nil {
				t.Fatal(err)
			}
			var calls []runtime.ModelCall
			var tools []string
			open := map[string]string{}
			err = ParseJSONL(t.Context(), bytes.NewReader(raw), 1<<20, func(event Event) error {
				switch {
				case event.Kind == EventModelCall:
					calls = append(calls, *event.ModelCall)
				case event.ToolPhase == "started":
					tools = append(tools, event.ToolName)
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
			if len(calls) != tc.wantCalls {
				t.Fatalf("calls = %d, want %d: %+v", len(calls), tc.wantCalls, calls)
			}
			var partial int
			var main, all [2]int64
			for _, call := range calls {
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
				// A subagent's final call has exact usage but no finish reason, and result.usage excludes it.
				if call.StopReason == "" {
					continue
				}
				main[0] += call.InputTokens
				main[1] += call.OutputTokens
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

func TestSubagentModelCalls(t *testing.T) {
	type call struct {
		input, output int64
		partial       bool
		start         int64
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
			want: []call{{input: 7, output: 3, start: 100}},
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
			want: []call{{input: 5, output: 4, partial: true, start: 100}, {input: 9, output: 7, start: 102}},
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
				got = append(got, call{input: c.InputTokens, output: c.OutputTokens, partial: c.UsagePartial, start: c.Start.Unix()})
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
