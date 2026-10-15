package a2a

import (
	"context"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

// TestFinalTaskStatusCarriesTheTurnsUsage guards what clients read a turn's
// tokens and cost from: the usage a runtime reports on its Outcome lands under
// kagent.dev/a2a/usage on the final task status of a completed and a failed turn.
func TestFinalTaskStatusCarriesTheTurnsUsage(t *testing.T) {
	for _, failure := range []*runtime.Failure{nil, {Message: "boom"}} {
		cost := 0.25
		executor, err := New(fakeRunner{run: func(context.Context, runtime.Turn, runtime.EventSink) (runtime.Outcome, error) {
			return runtime.Outcome{
				Failure: failure,
				Usage:   &runtime.Usage{TotalCostUSD: &cost, NumTurns: 3, InputTokens: 120, CachedTokens: 20, OutputTokens: 40},
			}, nil
		}}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
		if err != nil {
			t.Fatal(err)
		}
		events, errs := collect(executor.Execute(t.Context(), requestContext("task-1", "hello")))
		if len(errs) != 0 {
			t.Fatal(errs)
		}
		last, ok := events[len(events)-1].(*a2atype.TaskStatusUpdateEvent)
		if !ok || last.Status.Message == nil {
			t.Fatalf("last event = %#v, want a final status with a message", events[len(events)-1])
		}
		usage, _ := last.Status.Message.Metadata[apia2a.UsageMetadataKey].(map[string]any)
		if usage["promptTokenCount"] != 120 || usage["candidatesTokenCount"] != 40 || usage["costUsd"] != 0.25 {
			t.Fatalf("usage metadata = %#v (failure %v)", usage, failure)
		}
	}
}
