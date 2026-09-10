package server

import (
	"context"
	"log/slog"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	"go.opentelemetry.io/otel/trace"
)

// traceFlushInterceptor runs before a response reaches either A2A transport.
// The gateway quiesces on the task event, not on HTTP EOF. End the request
// span here so the batch flush includes it before the actor can be stopped.
// otelhttp's subsequent End is safe and has no effect on an already-ended span.
type traceFlushInterceptor struct {
	a2asrv.PassthroughCallInterceptor
	logger *slog.Logger
}

func (i *traceFlushInterceptor) After(ctx context.Context, _ *a2asrv.CallContext, response *a2asrv.Response) error {
	var state a2atype.TaskState
	switch payload := response.Payload.(type) {
	case *a2atype.TaskStatusUpdateEvent:
		state = payload.Status.State
	case *a2atype.Task:
		state = payload.Status.State
	default:
		return nil
	}
	if !state.Terminal() && state != a2atype.TaskStateInputRequired && state != a2atype.TaskStateAuthRequired {
		return nil
	}
	span := trace.SpanFromContext(ctx)
	span.End()
	if err := tracing.ForceFlush(ctx); err != nil {
		i.logger.ErrorContext(ctx, "failed to flush traces before quiescent A2A response", "error", err, "trace_id", span.SpanContext().TraceID().String(), "task_state", state)
	}
	return nil
}
