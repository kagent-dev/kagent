package translator_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/core/v2/translator"
)

func TestOtelEnvFromProcess(t *testing.T) {
	t.Setenv("OTEL_TRACING_ENABLED", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://collector:4317")
	t.Setenv("KAGENT_NOT_FORWARDED", "value")

	got := translator.OtelEnvFromProcess()

	values := map[string]string{}
	for _, variable := range got {
		if !strings.HasPrefix(variable.Name, "OTEL_") {
			t.Errorf("forwarded non-OTEL variable %q", variable.Name)
		}
		values[variable.Name] = variable.Value
	}
	if values["OTEL_TRACING_ENABLED"] != "true" {
		t.Errorf("OTEL_TRACING_ENABLED = %q, want %q", values["OTEL_TRACING_ENABLED"], "true")
	}
	if values["OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"] != "http://collector:4317" {
		t.Errorf("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT = %q", values["OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"])
	}
	if _, ok := values["KAGENT_NOT_FORWARDED"]; ok {
		t.Error("forwarded a variable outside the OTEL_ prefix")
	}

	names := make([]string, 0, len(got))
	for _, variable := range got {
		names = append(names, variable.Name)
	}
	if !slices.IsSorted(names) {
		t.Errorf("variables are not sorted by name: %v", names)
	}
}
