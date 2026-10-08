// Package telemetrytest checks the shape of exported telemetry in tests.
package telemetrytest

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

// Limits from the Instrumentation Score span rules (SPA-001, SPA-005).
const (
	maxInternalSpansPerService = 10
	maxShortSpans              = 20
	shortSpan                  = 5 * time.Millisecond
)

// AssertTraceShape fails t when spans break the span hygiene rules: an orphan,
// a CLIENT or PRODUCER span with no parent, an ERROR without a message, more
// than ten INTERNAL spans for one service in a trace, or more than twenty
// spans under five milliseconds in a trace.
func AssertTraceShape(t testing.TB, spans tracetest.SpanStubs) {
	t.Helper()
	known := map[trace.SpanID]bool{}
	for _, span := range spans {
		known[span.SpanContext.SpanID()] = true
	}
	internal := map[string]int{}
	short := map[trace.TraceID]int{}
	for _, span := range spans {
		parent := span.Parent
		switch {
		case parent.IsValid() && !parent.IsRemote() && !known[parent.SpanID()]:
			t.Errorf("span %q is an orphan: parent %s was not exported", span.Name, parent.SpanID())
		case !parent.IsValid() && (span.SpanKind == trace.SpanKindClient || span.SpanKind == trace.SpanKindProducer):
			t.Errorf("%s span %q has no parent", span.SpanKind, span.Name)
		}
		if span.Status.Code == codes.Error && span.Status.Description == "" {
			t.Errorf("span %q is ERROR without a message", span.Name)
		}
		traceID := span.SpanContext.TraceID()
		if span.SpanKind == trace.SpanKindInternal {
			internal[fmt.Sprintf("%s/%s", traceID, serviceName(span))]++
		}
		if span.EndTime.Sub(span.StartTime) < shortSpan {
			short[traceID]++
		}
	}
	for key, count := range internal {
		if count > maxInternalSpansPerService {
			t.Errorf("trace/service %s has %d INTERNAL spans, want at most %d", key, count, maxInternalSpansPerService)
		}
	}
	for traceID, count := range short {
		if count > maxShortSpans {
			t.Errorf("trace %s has %d spans under %s, want at most %d", traceID, count, shortSpan, maxShortSpans)
		}
	}
}

func serviceName(span tracetest.SpanStub) string {
	if span.Resource == nil {
		return ""
	}
	value, _ := span.Resource.Set().Value(semconv.ServiceNameKey)
	return value.AsString()
}

// MetricShape is the contract of one metric: its name, instrument kind, unit
// and the sorted set of attribute keys across its data points.
type MetricShape struct {
	Name          string
	Kind          string
	Unit          string
	AttributeKeys []string
}

// Collect reads everything reader holds.
func Collect(t testing.TB, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var data metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &data); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	return data
}

// FindMetric returns the shape of the named metric, and false when no scope
// produced it.
func FindMetric(data metricdata.ResourceMetrics, name string) (MetricShape, bool) {
	for _, scope := range data.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != name {
				continue
			}
			shape := MetricShape{Name: metric.Name, Unit: metric.Unit}
			var sets []attribute.Set
			switch data := metric.Data.(type) {
			case metricdata.Histogram[float64]:
				shape.Kind, sets = "histogram", histogramSets(data.DataPoints)
			case metricdata.Histogram[int64]:
				shape.Kind, sets = "histogram", histogramSets(data.DataPoints)
			case metricdata.Sum[float64]:
				shape.Kind, sets = "sum", dataPointSets(data.DataPoints)
			case metricdata.Sum[int64]:
				shape.Kind, sets = "sum", dataPointSets(data.DataPoints)
			case metricdata.Gauge[float64]:
				shape.Kind, sets = "gauge", dataPointSets(data.DataPoints)
			case metricdata.Gauge[int64]:
				shape.Kind, sets = "gauge", dataPointSets(data.DataPoints)
			default:
				shape.Kind = fmt.Sprintf("%T", data)
			}
			for _, set := range sets {
				for _, key := range set.ToSlice() {
					if !slices.Contains(shape.AttributeKeys, string(key.Key)) {
						shape.AttributeKeys = append(shape.AttributeKeys, string(key.Key))
					}
				}
			}
			slices.Sort(shape.AttributeKeys)
			return shape, true
		}
	}
	return MetricShape{}, false
}

func histogramSets[N int64 | float64](points []metricdata.HistogramDataPoint[N]) []attribute.Set {
	sets := make([]attribute.Set, 0, len(points))
	for _, point := range points {
		sets = append(sets, point.Attributes)
	}
	return sets
}

func dataPointSets[N int64 | float64](points []metricdata.DataPoint[N]) []attribute.Set {
	sets := make([]attribute.Set, 0, len(points))
	for _, point := range points {
		sets = append(sets, point.Attributes)
	}
	return sets
}
