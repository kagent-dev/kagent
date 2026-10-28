package runtime_test

import (
	"encoding/json"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"github.com/kagent-dev/kagent/go/harness/runtime"
)

func TestTraceOriginRoundTrip(t *testing.T) {
	state, err := trace.ParseTraceState("vendor=value")
	if err != nil {
		t.Fatal(err)
	}
	for _, flags := range []trace.TraceFlags{0, trace.FlagsSampled} {
		span := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: flags, TraceState: state,
		})
		data, err := json.Marshal(runtime.NewTraceOrigin(span))
		if err != nil {
			t.Fatal(err)
		}
		var restored runtime.TraceOrigin
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatal(err)
		}
		got := restored.SpanContext()
		if got.TraceID() != span.TraceID() || got.SpanID() != span.SpanID() || got.TraceFlags() != flags || got.TraceState().String() != state.String() {
			t.Fatalf("restored trace context differs: %s", data)
		}
	}
}

func TestTraceOriginLegacyAndCorruptData(t *testing.T) {
	type record struct {
		Origin runtime.TraceOrigin `json:"origin,omitzero"`
	}
	for _, data := range []string{`{}`, `{"origin":null}`} {
		var r record
		if err := json.Unmarshal([]byte(data), &r); err != nil || !r.Origin.IsZero() {
			t.Fatalf("legacy record %s: %#v, %v", data, r, err)
		}
	}
	data, err := json.Marshal(record{})
	if err != nil || string(data) != "{}" {
		t.Fatalf("zero origin should be omitted: %s, %v", data, err)
	}
	for _, data := range []string{
		`{}`, `{"traceparent":"invalid"}`,
		`{"traceparent":"00-00000000000000000000000000000000-0100000000000000-01"}`,
		`{"traceparent":"00-01000000000000000000000000000000-0000000000000000-01"}`,
		`{"traceparent":"00-01000000000000000000000000000000-0200000000000000-01","tracestate":"invalid"}`,
		`{"traceparent":"00-01000000000000000000000000000000-0200000000000000-01","unexpected":true}`,
	} {
		t.Run(data, func(t *testing.T) {
			origin := runtime.NewTraceOrigin(trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{3}, SpanID: trace.SpanID{4}}))
			before := origin.SpanContext()
			if err := json.Unmarshal([]byte(data), &origin); err == nil {
				t.Fatal("corrupt origin accepted")
			}
			if !origin.SpanContext().Equal(before) {
				t.Fatal("failed decode changed existing origin")
			}
		})
	}
}
