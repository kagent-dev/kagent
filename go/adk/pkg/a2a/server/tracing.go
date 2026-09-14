package server

import (
	"context"
	"log/slog"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// traceFlushInterceptor runs before a response reaches either A2A transport.
// The gateway quiesces on the task event, not on HTTP EOF. End the request
// span owned by this interceptor so the batch flush includes the invocation
// work before the actor can be stopped. The transport span remains owned by
// otelhttp, which records its response attributes before ending it.
type traceFlushInterceptor struct {
	a2asrv.PassthroughCallInterceptor
	logger *slog.Logger
}

func (i *traceFlushInterceptor) Before(ctx context.Context, callCtx *a2asrv.CallContext, _ *a2asrv.Request) (context.Context, any, error) {
	method := callCtx.Method()
	if method != "SendMessage" && method != "SendStreamingMessage" {
		return ctx, nil, nil
	}
	ctx, _ = otel.Tracer("github.com/kagent-dev/kagent/go/adk/pkg/a2a/server").Start(
		ctx,
		"a2a.request",
		trace.WithAttributes(attribute.String("a2a.method", method)),
	)
	return ctx, nil, nil
}

func (i *traceFlushInterceptor) After(ctx context.Context, callCtx *a2asrv.CallContext, response *a2asrv.Response) error {
	method := callCtx.Method()
	if method != "SendMessage" && method != "SendStreamingMessage" {
		return nil
	}

	var state a2atype.TaskState
	switch payload := response.Payload.(type) {
	case *a2atype.TaskStatusUpdateEvent:
		state = payload.Status.State
	case *a2atype.Task:
		state = payload.Status.State
	}
	quiescent := state.Terminal() || state == a2atype.TaskStateInputRequired || state == a2atype.TaskStateAuthRequired
	if method == "SendStreamingMessage" && response.Err == nil && !quiescent {
		return nil
	}
	span := trace.SpanFromContext(ctx)
	if state != a2atype.TaskStateUnspecified {
		span.SetAttributes(attribute.String("a2a.task.state", string(state)))
	}
	span.End()
	if !quiescent {
		return nil
	}
	if err := tracing.ForceFlush(ctx); err != nil {
		i.logger.ErrorContext(ctx, "failed to flush traces before quiescent A2A response", "error", err, "trace_id", span.SpanContext().TraceID().String(), "task_state", state)
	}
	return nil
}
