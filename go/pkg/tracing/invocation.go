package tracing

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type invocationKey struct{}

// Invocation is the request-scoped handle for the span that represents one A2A
// execution segment. The transport interceptor starts it and completes it while
// it still owns it. Execution takes ownership when it begins, so a unary
// response that returns while detached execution continues cannot report a
// finished invocation early. Completion runs exactly once no matter how many
// callbacks observe the same outcome.
//
// Every method tolerates a nil handle, so callers do not branch on whether
// tracing is installed.
type Invocation struct {
	span  trace.Span
	flush bool

	mu       sync.Mutex
	adopted  bool
	finished bool
}

// Result is the outcome recorded when an invocation completes. Error is a safe
// category such as "runtime_failure"; it never carries a provider response, a
// credential, or captured content.
type Result struct {
	TaskState   string
	Disposition string
	Error       string
}

// StartInvocation begins an invocation span and returns a context carrying its
// handle. Static attributes are applied at start so a request that fails before
// execution still reports the identity of the runtime that rejected it. Setting
// context attributes later cannot change a span that has already started.
//
// flush requests a bounded export when the invocation completes, for runtimes
// whose Actor may be suspended as soon as a quiescent event leaves the process.
func StartInvocation(ctx context.Context, tracer trace.Tracer, name string, flush bool, attributes ...attribute.KeyValue) (context.Context, *Invocation) {
	ctx, span := tracer.Start(ctx, name, trace.WithAttributes(attributes...))
	invocation := &Invocation{span: span, flush: flush}
	return context.WithValue(ctx, invocationKey{}, invocation), invocation
}

// InvocationFromContext returns the handle started for this request, or nil.
func InvocationFromContext(ctx context.Context) *Invocation {
	invocation, _ := ctx.Value(invocationKey{}).(*Invocation)
	return invocation
}

// Adopt transfers completion to the component that runs the request. The
// transport interceptor stops completing the invocation once it is adopted.
//
// It reports false when there is nothing to adopt, either because tracing is
// not installed or because the transport already completed the invocation.
// a2a-go dispatches execution before the caller's subscription is read, so a
// caller that disconnects in that window ends the invocation from the
// transport side and this execution is left untraced.
func (i *Invocation) Adopt() bool {
	if i == nil {
		return false
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.finished {
		return false
	}
	i.adopted = true
	return true
}

// SetAttributes records attributes on an invocation that has not completed.
func (i *Invocation) SetAttributes(attributes ...attribute.KeyValue) {
	if i == nil || len(attributes) == 0 {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.finished {
		return
	}
	i.span.SetAttributes(attributes...)
}

// AddLink relates this invocation to another span without reparenting either.
func (i *Invocation) AddLink(link trace.Link) {
	if i == nil || !link.SpanContext.IsValid() {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.finished {
		return
	}
	i.span.AddLink(link)
}

// SpanContext identifies this invocation for links recorded by later segments.
func (i *Invocation) SpanContext() trace.SpanContext {
	if i == nil {
		return trace.SpanContext{}
	}
	return i.span.SpanContext()
}

// IsRecording reports whether the invocation will retain what it is given.
// Content capture consults it before allocating any buffer.
func (i *Invocation) IsRecording() bool {
	return i != nil && i.span.IsRecording()
}

// End completes the invocation from the component that owns execution. It
// reports whether this call ended the span, and any export failure the caller
// should log.
func (i *Invocation) End(ctx context.Context, result Result) (bool, error) {
	return i.end(ctx, result, true)
}

// EndTransport completes the invocation from the transport interceptor. It does
// nothing once execution has adopted the invocation, so a response observed
// ahead of the execution boundary cannot close the segment early.
func (i *Invocation) EndTransport(ctx context.Context, result Result) (bool, error) {
	return i.end(ctx, result, false)
}

func (i *Invocation) end(ctx context.Context, result Result, owner bool) (bool, error) {
	if i == nil {
		return false, nil
	}
	i.mu.Lock()
	if i.finished || (!owner && i.adopted) {
		i.mu.Unlock()
		return false, nil
	}
	i.finished = true
	attributes := make([]attribute.KeyValue, 0, 3)
	if result.TaskState != "" {
		attributes = append(attributes, attribute.String(AttributeTaskState, result.TaskState))
	}
	if result.Disposition != "" {
		attributes = append(attributes, attribute.String(AttributeDisposition, result.Disposition))
	}
	if result.Error != "" {
		attributes = append(attributes, attribute.String(AttributeErrorType, result.Error))
	}
	i.span.SetAttributes(attributes...)
	if result.Error != "" {
		i.span.SetStatus(codes.Error, result.Error)
	}
	i.span.End()
	flush := i.flush
	i.mu.Unlock()
	if !flush {
		return true, nil
	}
	// ForceFlush detaches from caller cancellation and bounds its own wait, so a
	// collector outage costs at most that budget once per segment.
	return true, ForceFlush(ctx)
}
