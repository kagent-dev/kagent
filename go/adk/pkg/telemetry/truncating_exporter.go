package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// maxSpanAttributeBytes caps individual string span attributes before export.
// Tempo's default gRPC message limit is 4 MB; large tool responses can exceed it.
const maxSpanAttributeBytes = 16 * 1024

type truncatingExporter struct {
	inner sdktrace.SpanExporter
}

func newTruncatingExporter(inner sdktrace.SpanExporter) sdktrace.SpanExporter {
	return &truncatingExporter{inner: inner}
}

func (e *truncatingExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	out := make([]sdktrace.ReadOnlySpan, len(spans))
	for i, s := range spans {
		out[i] = truncateSpan(s)
	}
	return e.inner.ExportSpans(ctx, out)
}

func (e *truncatingExporter) Shutdown(ctx context.Context) error {
	return e.inner.Shutdown(ctx)
}

func truncateSpan(s sdktrace.ReadOnlySpan) sdktrace.ReadOnlySpan {
	attrs := s.Attributes()
	needsCut := false
	for _, a := range attrs {
		if a.Value.Type() == attribute.STRING && len(a.Value.AsString()) > maxSpanAttributeBytes {
			needsCut = true
			break
		}
	}
	if !needsCut {
		return s
	}
	newAttrs := make([]attribute.KeyValue, len(attrs))
	for i, a := range attrs {
		if a.Value.Type() == attribute.STRING {
			v := a.Value.AsString()
			if len(v) > maxSpanAttributeBytes {
				newAttrs[i] = attribute.String(string(a.Key),
					v[:maxSpanAttributeBytes]+fmt.Sprintf(" ...[truncated, %d bytes omitted]", len(v)-maxSpanAttributeBytes))
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
