package taskstore

import (
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/pkg/telemetry/conv"
)

func taskWithUsage(t *testing.T, state a2atype.TaskState, usage *apia2a.Usage) *a2atype.Task {
	t.Helper()
	task := &a2atype.Task{ID: "task", Status: a2atype.TaskStatus{State: state}, Metadata: map[string]any{}}
	if usage != nil {
		require.NoError(t, apia2a.AttachUsage(task.Metadata, *usage))
	}
	return task
}

func newTestUsageRecorder(t *testing.T) (*UsageRecorder, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	recorder, err := NewUsageRecorder(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)
	return recorder, reader
}

// collectUsage returns the counter values per metric and model, and the
// sender kind of every data point that carries one.
func collectUsage(t *testing.T, reader *sdkmetric.ManualReader) (map[string]map[string]int64, []string) {
	t.Helper()
	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &collected))
	values := map[string]map[string]int64{}
	var senders []string
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, m.Name)
			require.True(t, sum.IsMonotonic, m.Name)
			for _, point := range sum.DataPoints {
				agent, _ := point.Attributes.Value(conv.GenAIAgentIDKey)
				require.Equal(t, "team-a/assistant", agent.AsString())
				_, hasUser := point.Attributes.Value("enduser.id")
				require.False(t, hasUser, "the counters carry no user")
				if sender, ok := point.Attributes.Value(conv.KagentTurnSenderKey); ok {
					senders = append(senders, sender.AsString())
				}
				model, _ := point.Attributes.Value(conv.GenAIResponseModelKey)
				if values[m.Name] == nil {
					values[m.Name] = map[string]int64{}
				}
				values[m.Name][model.AsString()] = point.Value
			}
		}
	}
	return values, senders
}

var usageTestSession = &apiv1.Session{Id: "session", Agent: &apiv1.ResourceReference{Namespace: "team-a", Name: "assistant"}}

func TestSubtractUsageChargesWhatTheTurnAdded(t *testing.T) {
	before := apia2a.Usage{
		TokenCounts: apia2a.TokenCounts{InputTokens: 100, OutputTokens: 10, TotalTokens: 110},
		Models:      []apia2a.ModelUsage{{Model: "a", TokenCounts: apia2a.TokenCounts{InputTokens: 100, OutputTokens: 10, TotalTokens: 110}}},
	}
	after := apia2a.Usage{
		TokenCounts: apia2a.TokenCounts{InputTokens: 350, OutputTokens: 40, CachedInputTokens: 80, TotalTokens: 390},
		Models: []apia2a.ModelUsage{
			{Model: "a", TokenCounts: apia2a.TokenCounts{InputTokens: 100, OutputTokens: 10, TotalTokens: 110}},
			{Model: "b", TokenCounts: apia2a.TokenCounts{InputTokens: 250, OutputTokens: 30, CachedInputTokens: 80, TotalTokens: 280}},
		},
	}
	require.Equal(t, apia2a.Usage{
		TokenCounts: apia2a.TokenCounts{InputTokens: 250, OutputTokens: 30, CachedInputTokens: 80, TotalTokens: 280},
		Models:      []apia2a.ModelUsage{{Model: "b", TokenCounts: apia2a.TokenCounts{InputTokens: 250, OutputTokens: 30, CachedInputTokens: 80, TotalTokens: 280}}},
	}, subtractUsage(after, before))

	require.True(t, subtractUsage(before, after).IsZero(), "a total that went down charges nothing")
}

func TestUsageRecorderChargesWhatTheBoundaryAddedToItsSender(t *testing.T) {
	prior := apia2a.Usage{
		TokenCounts: apia2a.TokenCounts{InputTokens: 100, OutputTokens: 10, TotalTokens: 110},
		Models:      []apia2a.ModelUsage{{Model: "model-1", TokenCounts: apia2a.TokenCounts{InputTokens: 100, OutputTokens: 10, TotalTokens: 110}}},
	}
	final := apia2a.Usage{
		TokenCounts: apia2a.TokenCounts{InputTokens: 400, OutputTokens: 50, ReasoningTokens: 5, CachedInputTokens: 80, TotalTokens: 455},
		Models:      []apia2a.ModelUsage{{Model: "model-1", TokenCounts: apia2a.TokenCounts{InputTokens: 350, OutputTokens: 40, CachedInputTokens: 80, TotalTokens: 390}}},
	}
	for _, kind := range []string{conv.KagentTurnSenderPrincipal, conv.KagentTurnSenderActor, conv.KagentTurnSenderController} {
		t.Run(kind, func(t *testing.T) {
			recorder, reader := newTestUsageRecorder(t)
			recorder.record(t.Context(), usageTestSession, &database.SettledTask{
				Previous: taskWithUsage(t, a2atype.TaskStateWorking, &prior),
				Task:     taskWithUsage(t, a2atype.TaskStateCompleted, &final),
				Sender:   &database.TurnSender{Kind: kind, PrincipalID: "alice"},
			})

			values, senders := collectUsage(t, reader)
			require.Equal(t, map[string]map[string]int64{
				"kagent.turn.usage.input_tokens":            {"model-1": 250, "": 50},
				"kagent.turn.usage.output_tokens":           {"model-1": 30, "": 10},
				"kagent.turn.usage.reasoning.output_tokens": {"model-1": 0, "": 5},
				"kagent.turn.usage.cache_read.input_tokens": {"model-1": 80, "": 0},
			}, values)
			for _, sender := range senders {
				require.Equal(t, kind, sender)
			}
		})
	}
}

func TestUsageRecorderCountsNothingWithoutUsage(t *testing.T) {
	usage := apia2a.Usage{TokenCounts: apia2a.TokenCounts{InputTokens: 10, TotalTokens: 10}}
	for _, tt := range []struct {
		name    string
		settled *database.SettledTask
	}{
		{name: "already published"},
		{name: "runtime reports no usage", settled: &database.SettledTask{
			Previous: taskWithUsage(t, a2atype.TaskStateWorking, nil),
			Task:     taskWithUsage(t, a2atype.TaskStateCompleted, nil),
			Sender:   &database.TurnSender{Kind: conv.KagentTurnSenderPrincipal, PrincipalID: "alice"},
		}},
		{name: "no usage added", settled: &database.SettledTask{
			Previous: taskWithUsage(t, a2atype.TaskStateWorking, &usage),
			Task:     taskWithUsage(t, a2atype.TaskStateCompleted, &usage),
			Sender:   &database.TurnSender{Kind: conv.KagentTurnSenderPrincipal, PrincipalID: "alice"},
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			recorder, reader := newTestUsageRecorder(t)
			recorder.record(t.Context(), usageTestSession, tt.settled)
			values, _ := collectUsage(t, reader)
			require.Empty(t, values)
		})
	}
}

func TestUsageRecorderCountsTokensWithoutARecordedSender(t *testing.T) {
	usage := apia2a.Usage{TokenCounts: apia2a.TokenCounts{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}}
	recorder, reader := newTestUsageRecorder(t)
	recorder.record(t.Context(), usageTestSession, &database.SettledTask{
		Previous: taskWithUsage(t, a2atype.TaskStateWorking, nil),
		Task:     taskWithUsage(t, a2atype.TaskStateCompleted, &usage),
	})

	values, senders := collectUsage(t, reader)
	require.Equal(t, int64(10), values["kagent.turn.usage.input_tokens"][""])
	require.Equal(t, int64(2), values["kagent.turn.usage.output_tokens"][""])
	require.Empty(t, senders)
}

func TestSettleTaskChargesTheSettledBoundary(t *testing.T) {
	usage := apia2a.Usage{TokenCounts: apia2a.TokenCounts{InputTokens: 120, OutputTokens: 30, TotalTokens: 150}}
	commit := make(chan struct{}, 1)
	commit <- struct{}{}
	store := &settlementStore{commit: commit, settled: &database.SettledTask{
		Previous: taskWithUsage(t, a2atype.TaskStateWorking, nil),
		Task:     taskWithUsage(t, a2atype.TaskStateCompleted, &usage),
		Sender:   &database.TurnSender{Kind: conv.KagentTurnSenderController, PrincipalID: "alice"},
	}}
	recorder, reader := newTestUsageRecorder(t)
	service := NewService(store, nil, nil, recorder)

	_, err := service.SettleTask(runtimeContext(t), &runtimev1.TaskStoreServiceSettleTaskRequest{SessionId: "00000000-0000-0000-0000-000000000001", TaskId: "task", Version: 1})
	require.NoError(t, err)

	values, senders := collectUsage(t, reader)
	require.Equal(t, int64(120), values["kagent.turn.usage.input_tokens"][""])
	require.Equal(t, int64(30), values["kagent.turn.usage.output_tokens"][""])
	require.Equal(t, []string{conv.KagentTurnSenderController, conv.KagentTurnSenderController, conv.KagentTurnSenderController, conv.KagentTurnSenderController}, senders)
}
