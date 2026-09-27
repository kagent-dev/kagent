package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestNew(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(&output, "warn")
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("hidden")
	logger.Warn("visible", "task_id", "task-1")

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["level"] != "WARN" || record["msg"] != "visible" || record["task_id"] != "task-1" {
		t.Fatalf("unexpected record: %#v", record)
	}
}

func TestContextBridge(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil)).With("component", "test")
	FromContext(IntoContext(context.Background(), logger)).Info("bridged")

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["component"] != "test" {
		t.Fatalf("context fields were lost: %#v", record)
	}
}

func TestNewRejectsInvalidLevel(t *testing.T) {
	if _, err := New(&bytes.Buffer{}, "verbose"); err == nil {
		t.Fatal("New accepted invalid level")
	}
}

func TestNewAddsTraceContextInsideASpan(t *testing.T) {
	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled,
	}))
	tests := []struct {
		name string
		ctx  context.Context
		want map[string]any
	}{
		{
			name: "inside a span",
			ctx:  ctx,
			want: map[string]any{"trace_id": "4bf92f3577b34da6a3ce929d0e0e4736", "span_id": "00f067aa0ba902b7", "trace_flags": "01"},
		},
		{
			name: "outside a span",
			ctx:  context.Background(),
			want: map[string]any{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			logger, err := New(&output, "info")
			if err != nil {
				t.Fatal(err)
			}
			FromContext(IntoContext(tt.ctx, logger.With("component", "test"))).InfoContext(tt.ctx, "correlated")

			var record map[string]any
			if err := json.Unmarshal(output.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"trace_id", "span_id", "trace_flags"} {
				if record[key] != tt.want[key] {
					t.Errorf("%s = %v, want %v", key, record[key], tt.want[key])
				}
			}
			if record["component"] != "test" {
				t.Errorf("logger fields were lost: %#v", record)
			}
		})
	}
}
