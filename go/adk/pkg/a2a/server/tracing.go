package server

import (
	"context"
	"log/slog"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/adk/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	"go.opentelemetry.io/otel/attribute"
)

// invocationScope is the instrumentation scope of the invocation span.
// Consumers may key on it, so it stays stable.
const invocationScope = "github.com/kagent-dev/kagent/go/adk/pkg/a2a/server"

// invocationInterceptor anchors one A2A execution segment on a request span.
// For a native harness that span is the GenAI conventions' invoke_agent
// operation, since nothing beneath it describes the agent invocation; the ADK
// emits its own invoke_agent, so its request span stays a transport span with
// the same identity and no operation of its own. The interceptor starts the
// span with the identity the runtime knows before execution begins, so a
// request rejected during validation still reports which agent rejected it,
// and it completes the span for failures that never reach an executor.
//
// Execution adopts the invocation when it starts. From that point this
// interceptor stops completing it, because a unary response can return while
// a2a-go runs the executor detached from the caller and a transport response
// must not report a finished invocation while execution continues.
//
// When flush is set the gateway may suspend the Actor as soon as a quiescent
// event leaves the process, so completion exports before that event is yielded.
// The transport span stays owned by otelhttp, which records its response
// attributes before ending it.
type invocationInterceptor struct {
	a2asrv.PassthroughCallInterceptor
	logger    *slog.Logger
	telemetry tracing.RuntimeTelemetry
	flush     bool
}

// spanName follows the conventions for a native harness: the operation, then
// the agent name. Any other runtime keeps the transport span name.
func (i *invocationInterceptor) spanName() string {
	if !i.telemetry.Runtime.NativeHarness() {
		return tracing.TransportSpanName
	}
	return tracing.OperationInvokeAgent + " " + i.telemetry.AgentName
}

func (i *invocationInterceptor) Before(ctx context.Context, callCtx *a2asrv.CallContext, _ *a2asrv.Request) (context.Context, any, error) {
	method := callCtx.Method()
	if method != "SendMessage" && method != "SendStreamingMessage" {
		return ctx, nil, nil
	}
	identity := i.telemetry.Identity()
	attributes := make([]attribute.KeyValue, 0, len(identity)+3)
	if i.telemetry.Runtime.NativeHarness() {
		attributes = append(attributes, attribute.String(tracing.AttributeOperationName, tracing.OperationInvokeAgent))
	}
	attributes = append(attributes, attribute.String(tracing.AttributeMethod, method))
	attributes = append(attributes, identity...)
	// The gateway authenticates the caller and replaces x-user-id before
	// forwarding, so this is the only trusted identity on the private path.
	if userID := auth.UserIDFromContext(ctx); userID != "" {
		attributes = append(attributes, attribute.String(tracing.AttributeUserID, userID))
	}
	ctx, _ = tracing.StartInvocation(ctx, tracing.Tracer(invocationScope), i.spanName(), i.flush, attributes...)
	return ctx, nil, nil
}

func (i *invocationInterceptor) After(ctx context.Context, callCtx *a2asrv.CallContext, response *a2asrv.Response) error {
	method := callCtx.Method()
	if method != "SendMessage" && method != "SendStreamingMessage" {
		return nil
	}
	invocation := tracing.InvocationFromContext(ctx)
	if invocation == nil {
		return nil
	}

	var state a2atype.TaskState
	// A standalone agent message is a final A2A result that carries no task
	// state. Treating it as non-quiescent would leave the invocation open with
	// nothing left to complete it.
	final := false
	switch payload := response.Payload.(type) {
	case *a2atype.TaskStatusUpdateEvent:
		state = payload.Status.State
	case *a2atype.Task:
		state = payload.Status.State
	case *a2atype.Message:
		final = true
	}
	quiescent := final || state.Terminal() || state == a2atype.TaskStateInputRequired || state == a2atype.TaskStateAuthRequired
	if method == "SendStreamingMessage" && response.Err == nil && !quiescent {
		return nil
	}
	result := tracing.Result{TaskState: string(state)}
	if response.Err != nil {
		result.Error = "transport_error"
	}
	if _, err := invocation.EndTransport(ctx, result); err != nil {
		i.logger.ErrorContext(ctx, "failed to flush traces before quiescent A2A response", "error", err,
			"trace_id", invocation.SpanContext().TraceID().String(), "task_state", state)
	}
	return nil
}
