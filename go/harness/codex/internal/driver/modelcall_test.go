package driver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/harness/codex/config"
)

func TestModelCallsFromCaptures(t *testing.T) {
	for _, tc := range []struct {
		file         string
		wantCalls    int
		wantTools    int
		wantTerminal int
	}{
		{file: "codex-normal.jsonl", wantCalls: 1, wantTerminal: 1},
		{file: "codex-tools.jsonl", wantCalls: 4, wantTools: 3, wantTerminal: 1},
		// No usage is reported for an interrupted or rejected request; the killed process sent no terminal event.
		{file: "codex-cancel.jsonl"},
		{file: "codex-fail.jsonl", wantTerminal: 1},
	} {
		t.Run(tc.file, func(t *testing.T) {
			file, err := os.Open("testdata/modelcall/" + tc.file)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			sink := &recordingSink{}
			var translator *eventTranslator
			var threadID string
			var total codexUsage
			terminal := 0
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				var message rpcMessage
				if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
					t.Fatal(err)
				}
				switch string(message.ID) {
				case "2":
					if threadID, err = responseThreadID(message.Result); err != nil {
						t.Fatal(err)
					}
					continue
				case "3":
					turnID, err := responseTurnID(message.Result)
					if err != nil {
						t.Fatal(err)
					}
					translator = newEventTranslator(threadID, turnID)
					continue
				}
				if message.Method == "thread/tokenUsage/updated" {
					var params struct{ TokenUsage struct{ Total codexUsage } }
					if err := json.Unmarshal(message.Params, &params); err != nil {
						t.Fatal(err)
					}
					total = params.TokenUsage.Total
				}
				_, done, err := translator.translate(message, sink)
				if err != nil {
					t.Fatal(err)
				}
				if done {
					terminal++
				}
			}
			if err := scanner.Err(); err != nil {
				t.Fatal(err)
			}
			if terminal != tc.wantTerminal {
				t.Errorf("terminal events = %d, want %d", terminal, tc.wantTerminal)
			}
			if len(sink.calls) != tc.wantTools || len(sink.results) != tc.wantTools {
				t.Errorf("tool calls/results = %d/%d, want %d", len(sink.calls), len(sink.results), tc.wantTools)
			}
			for i := range min(len(sink.calls), len(sink.results)) {
				if call, result := sink.calls[i], sink.results[i]; result.ID != call.ID || result.Name != call.Name || call.Name != "command_execution" {
					t.Errorf("tool %d: call %s %q, result %s %q", i, call.ID, call.Name, result.ID, result.Name)
				}
			}
			if len(sink.models) != tc.wantCalls {
				t.Fatalf("calls = %d, want %d", len(sink.models), tc.wantCalls)
			}
			var sum codexUsage
			for _, call := range sink.models {
				// thread/start echoes the configured model, so it is not a response model.
				if call.ResponseModel != "" || call.UsagePartial || call.End.Before(call.Start) {
					t.Errorf("unexpected call %+v", call)
				}
				sum.InputTokens += call.InputTokens
				sum.CachedInputTokens += call.CacheReadTokens
				sum.OutputTokens += call.OutputTokens
			}
			if sum != total {
				t.Errorf("sum of last = %+v, want total %+v", sum, total)
			}
		})
	}
}

func usageMessage(t *testing.T, thread, turn string, total, last int64) rpcMessage {
	t.Helper()
	raw := fmt.Sprintf(`{"method":"thread/tokenUsage/updated","params":{"threadId":%q,"turnId":%q,"tokenUsage":{`+
		`"total":{"inputTokens":%d,"outputTokens":1},"last":{"inputTokens":%d,"outputTokens":1}}}}`, thread, turn, total, last)
	var message rpcMessage
	if err := json.Unmarshal([]byte(raw), &message); err != nil {
		t.Fatal(err)
	}
	return message
}

func TestModelCallsFromUsage(t *testing.T) {
	for _, tc := range []struct {
		name       string
		thread     string
		messages   []rpcMessage
		wantInputs []int64
	}{
		{
			name:   "subagent threads count",
			thread: "parent",
			messages: []rpcMessage{
				usageMessage(t, "parent", "turn", 10, 10),
				usageMessage(t, "child", "child-turn", 7, 7),
				usageMessage(t, "child", "child-turn", 12, 5),
			},
			wantInputs: []int64{10, 7, 5},
		},
		{
			// A repeated total is a replay; a grown total is a new identical call.
			name:   "replayed usage is skipped",
			thread: "thread",
			messages: []rpcMessage{
				usageMessage(t, "thread", "turn", 10, 10),
				usageMessage(t, "thread", "turn", 10, 10),
				usageMessage(t, "thread", "turn", 20, 10),
			},
			wantInputs: []int64{10, 10},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			translator := newEventTranslator(tc.thread, "turn")
			for _, message := range tc.messages {
				if _, _, err := translator.translate(message, sink); err != nil {
					t.Fatal(err)
				}
			}
			var got []int64
			for _, call := range sink.models {
				got = append(got, call.InputTokens)
			}
			if !slices.Equal(got, tc.wantInputs) {
				t.Fatalf("input tokens = %v, want %v", got, tc.wantInputs)
			}
		})
	}
}

func TestModelCallStartsAtThePreviousCallEnd(t *testing.T) {
	clock := time.Unix(100, 0)
	sink := &recordingSink{}
	translator := newEventTranslator("thread", "turn")
	translator.now = func() time.Time { return clock }
	translator.lastCallEnd = clock
	for _, step := range []struct {
		raw     string
		advance time.Duration
	}{
		{raw: `{"method":"item/started","params":{"threadId":"thread","turnId":"turn","item":{"type":"commandExecution","id":"c1","command":"ls","cwd":"/w","status":"inProgress"}}}`, advance: 5 * time.Second},
		{raw: `{"method":"item/completed","params":{"threadId":"thread","turnId":"turn","item":{"type":"commandExecution","id":"c1","command":"ls","cwd":"/w","status":"completed","exitCode":0}}}`, advance: 2 * time.Second},
	} {
		var message rpcMessage
		if err := json.Unmarshal([]byte(step.raw), &message); err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(step.advance)
		if _, _, err := translator.translate(message, sink); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := translator.translate(usageMessage(t, "thread", "turn", 10, 10), sink); err != nil {
		t.Fatal(err)
	}
	if got := sink.models[0]; !got.Start.Equal(time.Unix(100, 0)) || !got.End.Equal(time.Unix(107, 0)) {
		t.Fatalf("call = %s..%s, want the previous call end (100s) to the usage arrival (107s)", got.Start, got.End)
	}
}

func TestCapturesUseThePinnedCodexVersion(t *testing.T) {
	files, err := filepath.Glob("testdata/modelcall/*.jsonl")
	if err != nil || len(files) == 0 {
		t.Fatalf("captures = %v, %v", files, err)
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var start struct {
			Result struct {
				Thread struct {
					Version string `json:"cliVersion"`
				}
			}
		}
		line, _, _ := bytes.Cut(raw, []byte("\n"))
		if err := json.Unmarshal(line, &start); err != nil || start.Result.Thread.Version != config.PinnedCodexVersion {
			t.Errorf("%s cliVersion = %q (%v), want %s", file, start.Result.Thread.Version, err, config.PinnedCodexVersion)
		}
	}
}
