package e2e_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
)

type snapshotExperimentSample struct {
	Variant                string `json:"variant"`
	Harness                string `json:"harness"`
	Trial                  int    `json:"trial"`
	Turn                   int    `json:"turn"`
	Warmup                 bool   `json:"warmup"`
	TaskID                 string `json:"taskId,omitempty"`
	InstanceID             string `json:"instanceId,omitempty"`
	StartedAt              string `json:"startedAt"`
	RequestToFirstOutputNS int64  `json:"requestToFirstOutputNs,omitempty"`
	LastOutputToTerminalNS int64  `json:"lastOutputToTerminalNs,omitempty"`
	RequestToTerminalNS    int64  `json:"requestToTerminalNs,omitempty"`
	TerminalState          string `json:"terminalState,omitempty"`
	Error                  string `json:"error,omitempty"`
}

func TestE2ESnapshotScopeExperiment(t *testing.T) {
	variant := os.Getenv("KAGENT_SNAPSHOT_EXPERIMENT_VARIANT")
	output := os.Getenv("KAGENT_SNAPSHOT_EXPERIMENT_OUTPUT")
	if variant == "" && output == "" {
		t.Skip("snapshot experiment is opt-in")
	}
	if variant != "data" && variant != "full" && variant != "golden-data" {
		t.Fatalf("KAGENT_SNAPSHOT_EXPERIMENT_VARIANT = %q, want data, full, or golden-data", variant)
	}
	if output == "" {
		t.Fatal("KAGENT_SNAPSHOT_EXPERIMENT_OUTPUT is required")
	}
	trials := 5
	if value := os.Getenv("KAGENT_SNAPSHOT_EXPERIMENT_TRIALS"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 10 {
			t.Fatalf("KAGENT_SNAPSHOT_EXPERIMENT_TRIALS = %q, want an integer from 1 to 10", value)
		}
		trials = parsed
	}

	file, err := os.OpenFile(output, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("create snapshot experiment output: %v", err)
	}
	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Errorf("close snapshot experiment output: %v", err)
		}
	})
	encoder := json.NewEncoder(file)

	target := interactionTarget(t)
	harness := os.Getenv("KAGENT_SNAPSHOT_EXPERIMENT_HARNESS")
	if harness == "" {
		harness = "codex"
	}
	var template, harnessName, marker string
	switch harness {
	case "codex":
		modelURL := reachableModelURL(t, startMockLLMServer(t, codexInteractionMocks, "mocks/invoke_codex_agent.json"))
		template = createCodexMockTemplate(t, modelURL)
		harnessName, marker = codexE2EHarness, "CODEX"
	case "claude":
		modelURL := reachableServerURL(t, startMockLLMServer(t, claudeInteractionMocks, "mocks/invoke_claude_agent.json"), "")
		template = createClaudeMockTemplate(t, modelURL)
		harnessName, marker = claudeE2EHarness, "CLAUDE"
	default:
		t.Fatalf("unsupported harness %q", harness)
	}
	t.Logf("variant=%s harness=%s template=%s", variant, harness, template)

	for trial := 1; trial <= trials; trial++ {
		t.Run(fmt.Sprintf("trial-%d", trial), func(t *testing.T) {
			fixture := newInteractionFixtureForHarnessTemplate(t, target, harnessName, template)
			t.Logf("instance=%s", fixture.instanceID)
			for turn := 0; turn <= 5; turn++ {
				if turn == 1 {
					snapshotExperimentMetrics(t, output, trial, "before")
				}
				prompt := "Return exactly " + marker + "_MOCK_SECOND."
				if turn == 0 {
					prompt = "Return exactly " + marker + "_MOCK_FIRST."
				}
				sample := measureSnapshotExperimentTurn(t, fixture, prompt)
				sample.Variant = variant
				sample.Harness = harness
				sample.InstanceID = fixture.instanceID
				sample.Trial = trial
				sample.Turn = turn
				sample.Warmup = turn == 0
				if err := encoder.Encode(sample); err != nil {
					t.Fatalf("write snapshot experiment sample: %v", err)
				}
				if sample.Error != "" {
					t.Fatal(sample.Error)
				}
				if sample.TerminalState != a2atype.TaskStateCompleted.String() {
					t.Fatalf("turn %d state = %s, want completed", turn, sample.TerminalState)
				}
			}
			snapshotExperimentMetrics(t, output, trial, "after")
		})
	}
}

func snapshotExperimentMetrics(t *testing.T, output string, trial int, phase string) {
	t.Helper()
	script := os.Getenv("KAGENT_SNAPSHOT_EXPERIMENT_METRICS_SCRIPT")
	if script == "" {
		return
	}
	dir := filepath.Join(output+".metrics", fmt.Sprintf("trial-%d", trial), phase)
	if result, err := exec.CommandContext(t.Context(), "bash", script, dir).CombinedOutput(); err != nil {
		t.Fatalf("capture metrics: %v: %s", err, result)
	}
}

func measureSnapshotExperimentTurn(t *testing.T, fixture *interactionFixture, prompt string) snapshotExperimentSample {
	t.Helper()
	_, request := newMessageRequest(t, prompt)
	started := time.Now()
	sample := snapshotExperimentSample{StartedAt: started.UTC().Format(time.RFC3339Nano)}
	stream, err := fixture.client.SendStreamingMessage(fixture.ctx, request)
	if err != nil {
		sample.Error = fmt.Sprintf("start streaming A2A message: %v", err)
		return sample
	}

	var firstOutput, lastOutput, terminal time.Time
	for {
		response, err := stream.Recv()
		received := time.Now()
		if errors.Is(err, io.EOF) {
			if terminal.IsZero() {
				sample.Error = "stream closed without a terminal event"
			}
			break
		}
		if err != nil {
			sample.Error = fmt.Sprintf("receive A2A event: %v", err)
			break
		}
		event, err := pbconv.FromProtoStreamResponse(response)
		if err != nil {
			sample.Error = fmt.Sprintf("decode A2A event: %v", err)
			break
		}
		if taskID := event.TaskInfo().TaskID; taskID != "" {
			sample.TaskID = string(taskID)
		}
		if snapshotExperimentEventHasOutput(event) {
			if firstOutput.IsZero() {
				firstOutput = received
			}
			lastOutput = received
		}
		switch event := event.(type) {
		case *a2atype.Task:
			if event.Status.State.Terminal() {
				terminal = received
				sample.TerminalState = event.Status.State.String()
			}
		case *a2atype.TaskStatusUpdateEvent:
			if event.Status.State.Terminal() {
				terminal = received
				sample.TerminalState = event.Status.State.String()
			}
		}
	}

	if sample.Error == "" && firstOutput.IsZero() {
		sample.Error = "stream completed without a content-bearing output event"
	}
	if !firstOutput.IsZero() {
		sample.RequestToFirstOutputNS = firstOutput.Sub(started).Nanoseconds()
	}
	if !terminal.IsZero() {
		sample.RequestToTerminalNS = terminal.Sub(started).Nanoseconds()
	}
	if !lastOutput.IsZero() && !terminal.IsZero() {
		sample.LastOutputToTerminalNS = terminal.Sub(lastOutput).Nanoseconds()
	}
	return sample
}

func snapshotExperimentEventHasOutput(event a2atype.Event) bool {
	switch event := event.(type) {
	case *a2atype.Message:
		return len(event.Parts) > 0
	case *a2atype.TaskArtifactUpdateEvent:
		return event.Artifact != nil && len(event.Artifact.Parts) > 0
	case *a2atype.TaskStatusUpdateEvent:
		return event.Status.Message != nil && len(event.Status.Message.Parts) > 0
	default:
		return false
	}
}
