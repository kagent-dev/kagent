package translator_test

import (
	"reflect"
	"testing"

	"github.com/kagent-dev/kagent/go/core/internal/translator"
	corev1 "k8s.io/api/core/v1"
)

func TestTraceConfigFromProcess(t *testing.T) {
	t.Setenv("OTEL_TRACING_ENABLED", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://generic:4318/otel")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://traces:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "grpc")

	got, err := translator.TraceConfigFromProcess()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.Endpoint != "http://traces:4317" || got.Protocol != "grpc" || got.CollectorHostname() != "traces" {
		t.Fatalf("TraceConfigFromProcess() = %#v", got)
	}
	wantEnvironment := []corev1.EnvVar{
		{Name: "OTEL_TRACING_ENABLED", Value: "true"},
		{Name: "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", Value: "http://traces:4317"},
		{Name: "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", Value: "grpc"},
	}
	if !reflect.DeepEqual(got.Environment(), wantEnvironment) {
		t.Errorf("Environment() = %#v, want %#v", got.Environment(), wantEnvironment)
	}
}

func TestTraceConfigFromProcessNormalizesGenericHTTPEndpoint(t *testing.T) {
	t.Setenv("OTEL_TRACING_ENABLED", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4318/otel/")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")

	got, err := translator.TraceConfigFromProcess()
	if err != nil {
		t.Fatal(err)
	}
	if got.Endpoint != "http://collector:4318/otel/v1/traces" || got.Protocol != "http/protobuf" || got.CollectorHostname() != "collector" {
		t.Fatalf("TraceConfigFromProcess() = %#v", got)
	}
}

func TestTraceConfigFromProcessDisabled(t *testing.T) {
	t.Setenv("OTEL_TRACING_ENABLED", "false")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "not a URL")

	got, err := translator.TraceConfigFromProcess()
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.Environment() != nil || got.CollectorHostname() != "" {
		t.Fatalf("TraceConfigFromProcess() = %#v", got)
	}
}

func TestTraceConfigFromProcessRejectsInvalidConfiguration(t *testing.T) {
	for _, test := range []struct {
		name, endpoint, protocol string
	}{
		{name: "missing endpoint", protocol: "grpc"},
		{name: "relative endpoint", endpoint: "collector:4317", protocol: "grpc"},
		{name: "unsupported protocol", endpoint: "http://collector:4317", protocol: "zipkin"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("OTEL_TRACING_ENABLED", "true")
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", test.endpoint)
			t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", test.protocol)
			if _, err := translator.TraceConfigFromProcess(); err == nil {
				t.Fatal("TraceConfigFromProcess() error = nil")
			}
		})
	}
}
