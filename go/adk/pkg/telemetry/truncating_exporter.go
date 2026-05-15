package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

const (
	// maxSpanAttributeBytes is the maximum size of any single string span attribute.
	// Tempo's default gRPC message limit is 4MB; a single large tool response can
	// exceed that. Cap individual attributes to keep spans well within the limit.
	maxSpanAttributeBytes = 16 * 1024 // 16 KB
)

// truncatingExporter wraps a SpanExporter and truncates oversized string attributes
// before forwarding spans. This prevents large tool responses (kubectl output, YAML
// blobs, etc.) from producing spans that exceed Tempo's gRPC message size limit.
type truncatingExporter struct {
	inner sdktrace.SpanExporter
}

func newTruncatingExporter(inner sdktrace.SpanExporter) sdktrace.SpanExporter {
	return &truncatingExporter{inner: inner}
}

func (e *truncatingExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	truncated := make([]sdktrace.ReadOnlySpan, len(spans))
	for i, s := range spans {
		truncated[i] = truncateSpanAttributes(s)
	}
	return e.inner.ExportSpans(ctx, truncated)
}

func (e *truncatingExporter) Shutdown(ctx context.Context) error {
	return e.inner.Shutdown(ctx)
}

// truncateSpanAttributes returns a copy of the span with string attributes
// longer than maxSpanAttributeBytes replaced by a truncated version.
func truncateSpanAttributes(s sdktrace.ReadOnlySpan) sdktrace.ReadOnlySpan {
	attrs := s.Attributes()
	needsTruncation := false
	for _, a := range attrs {
		if a.Value.Type() == attribute.STRING && len(a.Value.AsString()) > maxSpanAttributeBytes {
			needsTruncation = true
			break
		}
	}
	if !needsTruncation {
		return s
	}

	newAttrs := make([]attribute.KeyValue, len(attrs))
	for i, a := range attrs {
		if a.Value.Type() == attribute.STRING {
			s := a.Value.AsString()
			if len(s) > maxSpanAttributeBytes {
				newAttrs[i] = attribute.String(string(a.Key), s[:maxSpanAttributeBytes]+
					fmt.Sprintf(" ...[truncated, %d bytes omitted]", len(s)-maxSpanAttributeBytes))
				continue
			}
		}
		newAttrs[i] = a
	}

	stub := tracetest.SpanStub{
		Name:                   s.Name(),
		SpanContext:            s.SpanContext(),
		Parent:                 s.Parent(),
		SpanKind:               s.SpanKind(),
		StartTime:              s.StartTime(),
		EndTime:                s.EndTime(),
		Attributes:             newAttrs,
		Events:                 s.Events(),
		Links:                  s.Links(),
		Status:                 s.Status(),
		DroppedAttributes:      s.DroppedAttributes(),
		DroppedEvents:          s.DroppedEvents(),
		DroppedLinks:           s.DroppedLinks(),
		ChildSpanCount:         s.ChildSpanCount(),
		Resource:               s.Resource(),
		InstrumentationLibrary: s.InstrumentationScope(),
	}
	return stub.Snapshot()
}
