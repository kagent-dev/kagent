package translator_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/core/internal/translator"
	corev1 "k8s.io/api/core/v1"
)

func TestTelemetryConfigFromProcess(t *testing.T) {
	clearTelemetryEnvironment(t)
	t.Setenv("OTEL_TRACING_ENABLED", "true")
	t.Setenv("OTEL_LOGGING_ENABLED", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://generic:4318/otel")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://traces:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "grpc")
	t.Setenv("KAGENT_OTEL_CAPTURE_SENSITIVE_CONTENT", "true")
	t.Setenv("KAGENT_OTEL_CAPTURE_RAW_API_BODIES", "true")

	got, warnings := translator.TelemetryConfigFromProcess()
	if len(warnings) != 0 {
		t.Fatalf("TelemetryConfigFromProcess() warnings = %v", warnings)
	}
	if !got.CaptureSensitiveContent || !got.CaptureRawAPIBodies {
		t.Fatalf("content capture = sensitive:%t raw:%t", got.CaptureSensitiveContent, got.CaptureRawAPIBodies)
	}
	if !got.Traces.Enabled || got.Traces.Endpoint != "http://traces:4317" || got.Traces.Protocol != "grpc" || got.Traces.Hostname != "traces" {
		t.Fatalf("traces = %#v", got.Traces)
	}
	if !got.Logs.Enabled || got.Logs.Endpoint != "http://generic:4318/otel/v1/logs" || got.Logs.Protocol != "http/protobuf" || got.Logs.Hostname != "generic" {
		t.Fatalf("logs = %#v", got.Logs)
	}
	if want := []corev1.EnvVar{
		{Name: "OTEL_TRACING_ENABLED", Value: "true"},
		{Name: "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", Value: "http://traces:4317"},
		{Name: "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", Value: "grpc"},
	}; !reflect.DeepEqual(got.TraceEnvironment(), want) {
		t.Errorf("trace environment = %#v, want %#v", got.TraceEnvironment(), want)
	}
	// The ADK runtimes read a plain true as log records only, so capture on is
	// rendered in the form that records span attributes.
	if capture, want := got.CaptureEnvironment(), (corev1.EnvVar{Name: "OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT", Value: "SPAN_ONLY"}); capture != want {
		t.Errorf("capture environment = %#v, want %#v", capture, want)
	}
	if want := []corev1.EnvVar{
		{Name: "OTEL_LOGGING_ENABLED", Value: "true"},
		{Name: "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", Value: "http://generic:4318/otel/v1/logs"},
		{Name: "OTEL_EXPORTER_OTLP_LOGS_PROTOCOL", Value: "http/protobuf"},
	}; !reflect.DeepEqual(got.LogEnvironment(), want) {
		t.Errorf("log environment = %#v, want %#v", got.LogEnvironment(), want)
	}
}

func TestTelemetryConfigFromProcessKeepsSignalsIndependent(t *testing.T) {
	clearTelemetryEnvironment(t)
	t.Setenv("OTEL_TRACING_ENABLED", "false")
	t.Setenv("OTEL_LOGGING_ENABLED", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "http://logs:4317")

	got, warnings := translator.TelemetryConfigFromProcess()
	if len(warnings) != 0 {
		t.Fatalf("TelemetryConfigFromProcess() warnings = %v", warnings)
	}
	if got.Traces.Enabled || got.TraceEnvironment() != nil || got.Traces.Hostname != "" || !got.Logs.Enabled {
		t.Fatalf("telemetry = %#v", got)
	}
	// The capture decision is rendered even when the controller exports no
	// traces, so a runtime cannot be handed a different one.
	if capture := got.CaptureEnvironment(); capture.Value != "false" {
		t.Fatalf("capture environment = %#v, want false", capture)
	}
}

func TestTelemetryConfigFromProcessDisablesInvalidSignals(t *testing.T) {
	clearTelemetryEnvironment(t)
	t.Setenv("OTEL_TRACING_ENABLED", "true")
	t.Setenv("OTEL_LOGGING_ENABLED", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "not a URL")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "http://logs:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL", "zipkin")

	got, warnings := translator.TelemetryConfigFromProcess()
	if got.Traces.Enabled || got.Logs.Enabled {
		t.Fatalf("invalid telemetry signals remain enabled: %#v", got)
	}
	if len(warnings) != 2 || !strings.Contains(warnings[0].Error(), "traces endpoint") || !strings.Contains(warnings[1].Error(), "logs protocol") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestTelemetryConfigFromProcessRejectsInvalidSignalConfiguration(t *testing.T) {
	for _, test := range []struct {
		name, endpoint, protocol string
	}{
		{name: "missing endpoint", protocol: "grpc"},
		{name: "relative endpoint", endpoint: "collector:4317", protocol: "grpc"},
		{name: "unsupported protocol", endpoint: "http://collector:4317", protocol: "zipkin"},
	} {
		t.Run(test.name, func(t *testing.T) {
			clearTelemetryEnvironment(t)
			t.Setenv("OTEL_TRACING_ENABLED", "true")
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", test.endpoint)
			t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", test.protocol)

			got, warnings := translator.TelemetryConfigFromProcess()
			if got.Traces.Enabled || len(warnings) != 1 {
				t.Fatalf("telemetry = %#v, warnings = %v", got, warnings)
			}
		})
	}
}

func TestTelemetryConfigFromProcessDefaultsContentCaptureOff(t *testing.T) {
	clearTelemetryEnvironment(t)
	got, warnings := translator.TelemetryConfigFromProcess()
	if len(warnings) != 0 {
		t.Fatalf("TelemetryConfigFromProcess() warnings = %v", warnings)
	}
	if got.CaptureSensitiveContent || got.CaptureRawAPIBodies {
		t.Fatalf("content capture defaults = sensitive:%t raw:%t", got.CaptureSensitiveContent, got.CaptureRawAPIBodies)
	}
}

func TestOwnsTelemetryEnvironment(t *testing.T) {
	for _, name := range []string{
		"OTEL_TRACING_ENABLED",
		"OTEL_LOGGING_ENABLED",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_PROTOCOL",
		"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL",
		"OTEL_EXPORTER_OTLP_LOGS_PROTOCOL",
		"OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT",
	} {
		if !translator.OwnsTelemetryEnvironment(name) {
			t.Errorf("OwnsTelemetryEnvironment(%q) = false", name)
		}
	}
	for _, name := range []string{"OTEL_BSP_SCHEDULE_DELAY", "OTEL_EXPORTER_OTLP_HEADERS", "OTEL_RESOURCE_ATTRIBUTES"} {
		if translator.OwnsTelemetryEnvironment(name) {
			t.Errorf("OwnsTelemetryEnvironment(%q) = true", name)
		}
	}
}

func clearTelemetryEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"OTEL_TRACING_ENABLED",
		"OTEL_LOGGING_ENABLED",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_PROTOCOL",
		"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL",
		"OTEL_EXPORTER_OTLP_LOGS_PROTOCOL",
		"KAGENT_OTEL_CAPTURE_SENSITIVE_CONTENT",
		"KAGENT_OTEL_CAPTURE_RAW_API_BODIES",
	} {
		t.Setenv(name, "")
	}
}
