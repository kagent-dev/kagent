package taskstore

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/telemetry/telemetrytest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

const turnsMetric = "kagent.agent.turns"

// settlingStore publishes each settled task once, like the database: a retried
// settlement of the same boundary returns no task.
type settlingStore struct {
	Store
	session   *apiv1alpha1.Session
	tasks     map[string]*a2a.Task
	published map[string]bool
}

func (s *settlingStore) GetSessionForRuntime(context.Context, string, string) (*apiv1alpha1.Session, error) {
	return s.session, nil
}

func (s *settlingStore) CreateRuntimeTask(context.Context, string, []byte, *a2a.Task, string) (int64, error) {
	return 1, nil
}

func (s *settlingStore) SettleSessionTaskResult(_ context.Context, _, taskID string, _ int64) (*a2a.Task, error) {
	if s.published[taskID] {
		return nil, nil
	}
	s.published[taskID] = true
	return s.tasks[taskID], nil
}

type turnKey struct{ agent, namespace, stoppedBy string }

func turnCounts(t *testing.T, reader *sdkmetric.ManualReader) map[turnKey]int64 {
	t.Helper()
	data := telemetrytest.Collect(t, reader)
	shape, found := telemetrytest.FindMetric(data, turnsMetric)
	require.True(t, found)
	require.Equal(t, "sum", shape.Kind)
	require.Equal(t, "{turn}", shape.Unit)
	counts := map[turnKey]int64{}
	for _, scope := range data.ScopeMetrics {
		for _, sample := range scope.Metrics {
			if sample.Name != turnsMetric {
				continue
			}
			sum, ok := sample.Data.(metricdata.Sum[int64])
			require.True(t, ok, "turns must be an integer sum")
			require.True(t, sum.IsMonotonic, "turns must be a counter")
			for _, point := range sum.DataPoints {
				value := func(key string) string {
					v, _ := point.Attributes.Value(attribute.Key(key))
					return v.AsString()
				}
				require.LessOrEqual(t, point.Attributes.Len(), 3, "no attribute beyond the agent and the limit")
				counts[turnKey{value("gen_ai.agent.name"), value("k8s.namespace.name"), value("kagent.turn.stopped_by")}] = point.Value
			}
		}
	}
	return counts
}

func completedTask(id string, stoppedBy string) *a2a.Task {
	task := &a2a.Task{ID: a2a.TaskID(id), Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}
	if stoppedBy != "" {
		message := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("stopped"))
		message.Metadata = map[string]any{apia2a.StoppedByMetadataKey: stoppedBy}
		task.Status.Message = message
	}
	return task
}

func TestSettledTurnsAreCountedByStoppedBy(t *testing.T) {
	sessionID := uuid.NewString()
	ctx := auth.AuthSessionTo(t.Context(), runtimeSession{sessionID: sessionID, atespace: "team-a", actorUID: "actor-uid"})
	store := &settlingStore{
		session: &apiv1alpha1.Session{
			Id:           sessionID,
			Agent:        &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "coder"},
			A2AAuthority: substrate.ActorHost("team-a", substrate.ActorName(sessionID), ""),
		},
		tasks: map[string]*a2a.Task{
			"natural": completedTask("natural", ""),
			"budget":  completedTask("budget", "budget"),
			"turns":   completedTask("turns", "turns"),
			"unknown": completedTask("unknown", "tokens"),
			"waiting": {ID: "waiting", Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired}},
			"failed":  {ID: "failed", Status: a2a.TaskStatus{State: a2a.TaskStateFailed}},
		},
		published: map[string]bool{},
	}
	registry := prometheus.NewRegistry()
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registry))
	require.NoError(t, err)
	reader := sdkmetric.NewManualReader()
	service, err := NewService(store, nil, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader), sdkmetric.WithReader(exporter)))
	require.NoError(t, err)

	wire, err := pbconv.ToProtoTask(&a2a.Task{ID: "natural", ContextID: uuid.NewString(), Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}})
	require.NoError(t, err)
	_, err = service.CreateTask(ctx, &apiv1alpha1.TaskStoreServiceCreateTaskRequest{SessionId: sessionID, Task: wire})
	require.NoError(t, err)
	require.Equal(t, map[turnKey]int64{
		{"coder", "team-a", ""}:       0,
		{"coder", "team-a", "budget"}: 0,
		{"coder", "team-a", "turns"}:  0,
	}, turnCounts(t, reader), "an Agent's series start at zero with its first task")

	settle := func(taskID string) {
		t.Helper()
		_, err := service.SettleTask(ctx, &apiv1alpha1.TaskStoreServiceSettleTaskRequest{SessionId: sessionID, TaskId: taskID, Version: 2})
		require.NoError(t, err)
	}
	for _, taskID := range []string{"natural", "budget", "budget", "turns", "unknown", "waiting", "failed"} {
		settle(taskID)
	}
	require.Equal(t, map[turnKey]int64{
		{"coder", "team-a", ""}:       2,
		{"coder", "team-a", "budget"}: 1,
		{"coder", "team-a", "turns"}:  1,
	}, turnCounts(t, reader), "a retried settlement counts once; an unknown limit counts as a turn that finished on its own; waiting and failed turns are not counted")

	response := httptest.NewRecorder()
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, response.Code)
	body := response.Body.String()
	require.Contains(t, body, "# TYPE kagent_agent_turns_total counter")
	require.Regexp(t, `kagent_agent_turns_total\{gen_ai_agent_name="coder",k8s_namespace_name="team-a",kagent_turn_stopped_by="budget",[^}]*\} 1`, body)
	require.Regexp(t, `kagent_agent_turns_total\{gen_ai_agent_name="coder",k8s_namespace_name="team-a",kagent_turn_stopped_by="turns",[^}]*\} 1`, body)
	require.Regexp(t, `kagent_agent_turns_total\{gen_ai_agent_name="coder",k8s_namespace_name="team-a",otel_scope_name="[^"]*taskstore"[^}]*\} 2`, body)
}
