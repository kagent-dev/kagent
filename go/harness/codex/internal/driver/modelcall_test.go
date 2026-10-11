package driver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/harness/codex/config"
	"github.com/kagent-dev/kagent/go/harness/runtime"
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

func TestModelCallTiming(t *testing.T) {
	started := func(id string) string {
		return `{"method":"item/started","params":{"threadId":"thread","turnId":"turn","item":{"type":"commandExecution","id":"` + id + `","command":"ls","cwd":"/w","status":"inProgress"}}}`
	}
	completed := func(id string) string {
		return `{"method":"item/completed","params":{"threadId":"thread","turnId":"turn","item":{"type":"commandExecution","id":"` + id + `","command":"ls","cwd":"/w","status":"completed","exitCode":0}}}`
	}
	type step struct {
		raw string // "" sends the next usage report
		at  int64
	}
	for _, tc := range []struct {
		name  string
		steps []step
		want  [][2]int64 // start and end of each call
	}{
		{
			name:  "a call without tools runs to its usage",
			steps: []step{{at: 104}},
			want:  [][2]int64{{100, 104}},
		},
		{
			// Usage arrives only after the tools ran, so the call ends at its first tool start.
			name:  "a call ends at its first tool start",
			steps: []step{{started("c1"), 105}, {started("c2"), 106}, {completed("c1"), 130}, {completed("c2"), 131}, {"", 132}},
			want:  [][2]int64{{100, 105}},
		},
		{
			name:  "the next call starts at the previous usage",
			steps: []step{{started("c1"), 105}, {completed("c1"), 130}, {"", 131}, {"", 135}},
			want:  [][2]int64{{100, 105}, {131, 135}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := time.Unix(100, 0)
			sink := &recordingSink{}
			translator := newEventTranslator("thread", "turn")
			translator.now = func() time.Time { return clock }
			translator.modelCallStart = clock
			reports := int64(0)
			for _, step := range tc.steps {
				clock = time.Unix(step.at, 0)
				message := rpcMessage{}
				if step.raw == "" {
					reports++
					message = usageMessage(t, "thread", "turn", 10*reports, 10*reports)
				} else if err := json.Unmarshal([]byte(step.raw), &message); err != nil {
					t.Fatal(err)
				}
				if _, _, err := translator.translate(message, sink); err != nil {
					t.Fatal(err)
				}
			}
			got := make([][2]int64, 0, len(sink.models))
			for _, call := range sink.models {
				got = append(got, [2]int64{call.Start.Unix(), call.End.Unix()})
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("calls = %v, want %v", got, tc.want)
			}
		})
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

func TestSubagentCallsNameTheirSpawnCall(t *testing.T) {
	collabOn := func(thread, method, id, tool string, receivers ...string) string {
		item, _ := json.Marshal(map[string]any{"type": "collabAgentToolCall", "id": id, "tool": tool, "receiverThreadIds": append([]string{}, receivers...), "status": "completed"})
		return fmt.Sprintf(`{"method":%q,"params":{"threadId":%q,"turnId":"turn","item":%s}}`, method, thread, item)
	}
	collab := func(method, id, tool string, receivers ...string) string {
		return collabOn("thread", method, id, tool, receivers...)
	}
	usage := func(thread string) string { return "usage:" + thread }
	command := func(thread, method, id string) string {
		return fmt.Sprintf(`{"method":%q,"params":{"threadId":%q,"turnId":"turn","item":{"type":"commandExecution","id":%q,"command":"ls","cwd":"/w","status":"completed","exitCode":0}}}`,
			method, thread, id)
	}
	for _, tc := range []struct {
		name            string
		steps           []string
		wantParents     []string
		wantToolParents []string             // parent of each tool call, in order; nil skips the check
		wantResults     []runtime.ToolStatus // status of each tool result, in order; nil skips the check
		wantErr         bool
	}{
		{
			name: "a spawned subagent",
			steps: []string{
				collab("item/started", "spawn-1", "spawnAgent"), collab("item/completed", "spawn-1", "spawnAgent", "child"),
				collab("item/started", "wait-1", "wait", "child"), usage("child"), collab("item/completed", "wait-1", "wait", "child"),
				usage("thread"),
			},
			wantParents: []string{"spawn-1", ""},
		},
		{
			name: "two subagents",
			steps: []string{
				collab("item/started", "spawn-1", "spawnAgent"), collab("item/completed", "spawn-1", "spawnAgent", "child-a"),
				collab("item/started", "spawn-2", "spawnAgent"), collab("item/completed", "spawn-2", "spawnAgent", "child-b"),
				usage("child-b"), usage("child-a"),
			},
			wantParents: []string{"spawn-2", "spawn-1"},
		},
		{
			// The child's own spawn call goes under the parent's spawn, and the grandchild under it.
			name: "a subagent's own subagent",
			steps: []string{
				collab("item/started", "spawn-1", "spawnAgent"), collab("item/completed", "spawn-1", "spawnAgent", "child"),
				collabOn("child", "item/started", "spawn-2", "spawnAgent"), collabOn("child", "item/completed", "spawn-2", "spawnAgent", "grandchild"),
				usage("grandchild"), usage("child"),
			},
			wantParents:     []string{"spawn-2", "spawn-1"},
			wantToolParents: []string{"", "spawn-1"},
		},
		{
			// Threads share one stream, so a child's tool can start before its spawn completes.
			name: "a child tool that starts before the spawn completes is dropped",
			steps: []string{
				collab("item/started", "spawn-1", "spawnAgent"), command("child", "item/started", "cmd-1"),
				collab("item/completed", "spawn-1", "spawnAgent", "child"), command("child", "item/completed", "cmd-1"),
				usage("child"),
			},
			wantParents:     []string{"spawn-1"},
			wantToolParents: []string{""},
		},
		{
			name: "a child tool that reuses an open main tool ID is dropped",
			steps: []string{
				command("thread", "item/started", "cmd-1"),
				collab("item/started", "spawn-1", "spawnAgent"), collab("item/completed", "spawn-1", "spawnAgent", "child"),
				command("child", "item/started", "cmd-1"), command("child", "item/completed", "cmd-1"),
				command("thread", "item/completed", "cmd-1"),
			},
			wantToolParents: []string{"", ""},
		},
		{
			name: "a main tool wins over a child tool with its ID",
			steps: []string{
				collab("item/started", "spawn-1", "spawnAgent"), collab("item/completed", "spawn-1", "spawnAgent", "child"),
				command("child", "item/started", "cmd-1"), command("thread", "item/started", "cmd-1"),
				command("child", "item/completed", "cmd-1"), command("thread", "item/completed", "cmd-1"),
			},
			wantToolParents: []string{"", "spawn-1", ""},
			wantResults:     []runtime.ToolStatus{runtime.ToolRan, runtime.ToolUnfinished, runtime.ToolRan},
		},
		{
			name: "a subagent cannot complete another subagent's tool",
			steps: []string{
				collab("item/started", "spawn-1", "spawnAgent"), collab("item/completed", "spawn-1", "spawnAgent", "first", "second"),
				command("first", "item/started", "cmd-1"), command("second", "item/completed", "cmd-1"),
			},
			wantToolParents: []string{"", "spawn-1"},
			wantResults:     []runtime.ToolStatus{runtime.ToolRan},
		},
		{
			name: "a subagent spawn that names its own thread claims nothing",
			steps: []string{
				collab("item/started", "spawn-1", "spawnAgent"), collab("item/completed", "spawn-1", "spawnAgent", "child"),
				collabOn("child", "item/started", "spawn-2", "spawnAgent"), collabOn("child", "item/completed", "spawn-2", "spawnAgent", "child"),
				usage("child"),
			},
			wantParents: []string{"spawn-1"},
		},
		{
			name: "the main thread cannot complete a subagent's tool",
			steps: []string{
				collab("item/started", "spawn-1", "spawnAgent"), collab("item/completed", "spawn-1", "spawnAgent", "child"),
				command("child", "item/started", "cmd-1"), command("thread", "item/completed", "cmd-1"),
			},
			wantErr: true,
		},
		{
			name: "a spawn that names the main thread claims nothing",
			steps: []string{
				collab("item/started", "spawn-1", "spawnAgent"), collab("item/completed", "spawn-1", "spawnAgent", "thread"),
				usage("thread"),
			},
			wantParents: []string{""},
		},
		{
			name: "wait and closeAgent do not claim a thread",
			steps: []string{
				collab("item/started", "wait-1", "wait", "child"), collab("item/completed", "wait-1", "wait", "child"),
				collab("item/started", "close-1", "closeAgent", "child"), collab("item/completed", "close-1", "closeAgent", "child"),
				usage("child"),
			},
			wantParents: []string{""},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			translator := newEventTranslator("thread", "turn")
			for i, raw := range tc.steps {
				var message rpcMessage
				if thread, ok := strings.CutPrefix(raw, "usage:"); ok {
					message = usageMessage(t, thread, "turn", int64(10*(i+1)), 10)
				} else if err := json.Unmarshal([]byte(raw), &message); err != nil {
					t.Fatal(err)
				}
				_, _, err := translator.translate(message, sink)
				if err != nil && tc.wantErr && i == len(tc.steps)-1 {
					return
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if tc.wantErr {
				t.Fatal("translate() accepted every step, want an error")
			}
			var parents []string
			for _, call := range sink.models {
				parents = append(parents, call.ParentToolID)
			}
			if !slices.Equal(parents, tc.wantParents) {
				t.Fatalf("parent tools = %q, want %q", parents, tc.wantParents)
			}
			var toolParents []string
			for _, call := range sink.calls {
				toolParents = append(toolParents, call.ParentToolID)
			}
			if tc.wantToolParents != nil && !slices.Equal(toolParents, tc.wantToolParents) {
				t.Fatalf("tool parents = %q, want %q", toolParents, tc.wantToolParents)
			}
			var results []runtime.ToolStatus
			for _, result := range sink.results {
				results = append(results, result.Status)
			}
			if tc.wantResults != nil && !slices.Equal(results, tc.wantResults) {
				t.Fatalf("tool results = %q, want %q", results, tc.wantResults)
			}
		})
	}
}
