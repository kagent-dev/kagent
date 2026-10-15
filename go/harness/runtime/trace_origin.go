package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TraceOrigin is the immutable span context of a task's initial invocation.
// Its zero value means no origin was recorded. Only W3C trace context is saved;
// it carries no span, Go context, baggage, or captured content.
type TraceOrigin struct{ spanContext trace.SpanContext }

type traceOriginWire struct {
	Traceparent string `json:"traceparent"`
	Tracestate  string `json:"tracestate,omitempty"`
}

func NewTraceOrigin(spanContext trace.SpanContext) TraceOrigin {
	if !spanContext.IsValid() {
		return TraceOrigin{}
	}
	return TraceOrigin{spanContext: spanContext}
}

func (o TraceOrigin) IsZero() bool { return !o.spanContext.IsValid() }

// SpanContext is used for links, never as the resumed invocation's parent.
func (o TraceOrigin) SpanContext() trace.SpanContext { return o.spanContext }

func (o TraceOrigin) MarshalJSON() ([]byte, error) {
	if o.IsZero() {
		return []byte("null"), nil
	}
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(trace.ContextWithSpanContext(context.Background(), o.spanContext), carrier)
	return json.Marshal(traceOriginWire{Traceparent: carrier.Get("traceparent"), Tracestate: carrier.Get("tracestate")})
}

func (o *TraceOrigin) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*o = TraceOrigin{}
		return nil
	}
	var wire traceOriginWire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return fmt.Errorf("decode trace origin: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("trace origin contains trailing data")
	}
	if _, err := trace.ParseTraceState(wire.Tracestate); err != nil {
		return fmt.Errorf("invalid trace origin tracestate: %w", err)
	}
	carrier := propagation.MapCarrier{"traceparent": wire.Traceparent, "tracestate": wire.Tracestate}
	ctx := propagation.TraceContext{}.Extract(context.Background(), carrier)
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() {
		return fmt.Errorf("invalid trace origin traceparent")
	}
	*o = TraceOrigin{spanContext: spanContext}
	return nil
}
