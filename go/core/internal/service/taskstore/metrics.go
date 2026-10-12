package taskstore

import (
	"context"
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/version"
	"github.com/kagent-dev/kagent/go/pkg/telemetry/conv"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

// stoppedByValues are the kagent.turn.stopped_by values a completed turn
// carries, the empty value for a turn that finished on its own.
var stoppedByValues = []string{"", conv.KagentTurnStoppedByBudget, conv.KagentTurnStoppedByTurns}

type turnMetrics struct {
	turns metric.Int64Counter
}

func newTurnMetrics(provider metric.MeterProvider) (*turnMetrics, error) {
	meter := provider.Meter("github.com/kagent-dev/kagent/go/core/internal/service/taskstore",
		metric.WithInstrumentationVersion(version.Version))
	turns, err := meter.Int64Counter(conv.KagentAgentTurns,
		metric.WithUnit(conv.KagentAgentTurnsUnit),
		metric.WithDescription(conv.KagentAgentTurnsDescription))
	if err != nil {
		return nil, fmt.Errorf("create agent turns metric: %w", err)
	}
	return &turnMetrics{turns: turns}, nil
}

// start creates the Agent's series at zero, so increase() sees its first
// completed turn.
func (m *turnMetrics) start(ctx context.Context, session *apiv1alpha1.Session) {
	agent := session.GetAgent()
	if agent == nil {
		return
	}
	for _, stoppedBy := range stoppedByValues {
		m.turns.Add(ctx, 0, metric.WithAttributes(turnAttributes(agent, stoppedBy)...))
	}
}

// settled counts a task the runtime settled as completed. Any other state is
// not a completed turn.
func (m *turnMetrics) settled(ctx context.Context, session *apiv1alpha1.Session, task *a2a.Task) {
	agent := session.GetAgent()
	if agent == nil || task == nil || task.Status.State != a2a.TaskStateCompleted {
		return
	}
	m.turns.Add(ctx, 1, metric.WithAttributes(turnAttributes(agent, stoppedBy(task))...))
}

// stoppedBy reads the limit the runtime recorded on the turn's final status.
// An unknown value is not a label: it counts as a turn that finished on its own.
func stoppedBy(task *a2a.Task) string {
	if task.Status.Message == nil {
		return ""
	}
	value, _ := task.Status.Message.Metadata[apia2a.StoppedByMetadataKey].(string)
	switch value {
	case conv.KagentTurnStoppedByBudget, conv.KagentTurnStoppedByTurns:
		return value
	default:
		return ""
	}
}

func turnAttributes(agent *apiv1alpha1.ResourceReference, stoppedBy string) []attribute.KeyValue {
	attributes := []attribute.KeyValue{
		conv.GenAIAgentNameKey.String(agent.GetName()),
		semconv.K8SNamespaceName(agent.GetNamespace()),
	}
	if stoppedBy != "" {
		attributes = append(attributes, conv.KagentTurnStoppedByKey.String(stoppedBy))
	}
	return attributes
}
