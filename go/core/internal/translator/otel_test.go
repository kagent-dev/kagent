package translator_test

import (
	"reflect"
	"testing"

	"github.com/kagent-dev/kagent/go/core/internal/translator"
	corev1 "k8s.io/api/core/v1"
)

func TestOtelEnvFromProcess(t *testing.T) {
	t.Setenv("OTEL_TRACING_ENABLED", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "collector:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://collector:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "grpc")
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "authorization=secret")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.name=controller")
	t.Setenv("OTEL_SERVICE_NAME", "controller")

	got := translator.OtelEnvFromProcess()
	want := []corev1.EnvVar{
		{Name: "OTEL_TRACING_ENABLED", Value: "true"},
		{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: "collector:4317"},
		{Name: "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", Value: "http://collector:4317"},
		{Name: "OTEL_EXPORTER_OTLP_PROTOCOL", Value: "http/protobuf"},
		{Name: "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", Value: "grpc"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("OtelEnvFromProcess() = %#v, want %#v", got, want)
	}
}
