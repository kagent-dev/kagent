package translator_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	corev1 "k8s.io/api/core/v1"
)

var testIdentity = tracing.RuntimeTelemetry{AgentName: "demo-claude", AgentNamespace: "team", Provider: "anthropic", Model: "claude-sonnet-5"}

func environmentMap(environment []corev1.EnvVar) map[string]string {
	values := make(map[string]string, len(environment))
	for _, variable := range environment {
		values[variable.Name] = variable.Value
	}
	return values
}

func TestTelemetryConfigFromProcess(t *testing.T) {
	clearTelemetryEnvironment(t)
	t.Setenv("OTEL_TRACES_EXPORTER", "otlp")
	t.Setenv("OTEL_METRICS_EXPORTER", "otlp")
	t.Setenv("OTEL_LOGS_EXPORTER", "otlp")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://generic:4318/otel")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://traces:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "grpc")
	t.Setenv("OTEL_EXPORTER_OTLP_TIMEOUT", "5000")
	t.Setenv("OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT", "SPAN_ONLY")
	t.Setenv("KAGENT_OTEL_CAPTURE_RAW_API_BODIES", "true")
	t.Setenv("KAGENT_OTEL_MAX_CAPTURE_BYTES", "4096")

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
	if !got.Logs.Enabled || got.Logs.Endpoint != "http://generic:4318/otel/v1/logs" || got.Logs.Protocol != "http/protobuf" {
		t.Fatalf("logs = %#v", got.Logs)
	}
	if want := []string{"traces", "generic", "generic"}; !reflect.DeepEqual(got.Destinations(), want) {
		t.Fatalf("destinations = %v, want %v", got.Destinations(), want)
	}
	want := []corev1.EnvVar{
		{Name: "OTEL_TRACES_EXPORTER", Value: "otlp"},
		{Name: "OTEL_METRICS_EXPORTER", Value: "otlp"},
		{Name: "OTEL_LOGS_EXPORTER", Value: "otlp"},
		{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: "http://generic:4318/otel"},
		{Name: "OTEL_EXPORTER_OTLP_PROTOCOL", Value: "http/protobuf"},
		{Name: "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", Value: "http://traces:4317"},
		{Name: "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", Value: "grpc"},
		{Name: "OTEL_EXPORTER_OTLP_TIMEOUT", Value: "5000"},
		{Name: "OTEL_SERVICE_NAME", Value: "demo-claude"},
		{Name: "OTEL_RESOURCE_ATTRIBUTES", Value: "gen_ai.agent.id=team/demo-claude,gen_ai.agent.name=demo-claude,gen_ai.provider.name=anthropic,gen_ai.request.model=claude-sonnet-5,service.namespace=team"},
		{Name: "OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT", Value: "SPAN_ONLY"},
	}
	if environment := got.TelemetryEnvironment(testIdentity, ""); !reflect.DeepEqual(environment, want) {
		t.Errorf("environment = %#v\nwant %#v", environment, want)
	}
}

func TestTelemetryEnvironmentRendersOnlyWhatChangesBehavior(t *testing.T) {
	clearTelemetryEnvironment(t)
	t.Setenv("OTEL_TRACES_EXPORTER", "otlp")
	t.Setenv("OTEL_METRICS_EXPORTER", "none")
	t.Setenv("OTEL_LOGS_EXPORTER", "none")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4317")

	got, warnings := translator.TelemetryConfigFromProcess()
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	environment := environmentMap(got.TelemetryEnvironment(testIdentity, ""))
	if environment["OTEL_EXPORTER_OTLP_PROTOCOL"] != "grpc" {
		t.Fatalf("protocol = %q, want the chart default rendered because SDK defaults differ", environment["OTEL_EXPORTER_OTLP_PROTOCOL"])
	}
	for _, name := range []string{
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "OTEL_EXPORTER_OTLP_TIMEOUT", "OTEL_SDK_DISABLED", "OTEL_SPAN_ATTRIBUTE_VALUE_LENGTH_LIMIT",
		"OTEL_PROPAGATORS", "OTEL_EXPORTER_OTLP_COMPRESSION", "OTEL_EXPORTER_OTLP_METRICS_DEFAULT_HISTOGRAM_AGGREGATION",
	} {
		if _, ok := environment[name]; ok {
			t.Errorf("%s rendered without a setting that needs it", name)
		}
	}
	if environment["OTEL_METRICS_EXPORTER"] != "none" || environment["OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT"] != "false" {
		t.Fatalf("environment = %v", environment)
	}
}

func TestTelemetryEnvironmentDisablesEverySDKWhenOff(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"unset":        {},
		"none":         {"OTEL_TRACES_EXPORTER": "none", "OTEL_METRICS_EXPORTER": "none", "OTEL_LOGS_EXPORTER": "none"},
		"sdk disabled": {"OTEL_TRACES_EXPORTER": "otlp", "OTEL_EXPORTER_OTLP_ENDPOINT": "http://collector:4317", "OTEL_SDK_DISABLED": "true"},
	} {
		t.Run(name, func(t *testing.T) {
			clearTelemetryEnvironment(t)
			for key, value := range env {
				t.Setenv(key, value)
			}
			got, _ := translator.TelemetryConfigFromProcess()
			want := []corev1.EnvVar{
				{Name: "OTEL_SDK_DISABLED", Value: "true"},
				{Name: "OTEL_TRACES_EXPORTER", Value: "none"},
				{Name: "OTEL_METRICS_EXPORTER", Value: "none"},
				{Name: "OTEL_LOGS_EXPORTER", Value: "none"},
				{Name: "OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT", Value: "false"},
			}
			if environment := got.TelemetryEnvironment(testIdentity, ""); !reflect.DeepEqual(environment, want) {
				t.Fatalf("environment = %#v", environment)
			}
			if got.Destinations() != nil {
				t.Fatalf("destinations = %v", got.Destinations())
			}
		})
	}
}

func TestTelemetryEnvironmentKeepsIdentityOverOperatorAndHarnessAttributes(t *testing.T) {
	clearTelemetryEnvironment(t)
	t.Setenv("OTEL_TRACES_EXPORTER", "otlp")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4317")
	t.Setenv(translator.OperatorResourceAttributesVariable, "deployment.environment.name=prod, service.namespace=forged,k8s.cluster.name=east")

	got, warnings := translator.TelemetryConfigFromProcess()
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	identity := tracing.RuntimeTelemetry{AgentName: "a-kagent", AgentNamespace: "team"}
	attributes := environmentMap(got.TelemetryEnvironment(identity, "k8s.cluster.name=west,department=eng,gen_ai.agent.name=forged"))["OTEL_RESOURCE_ATTRIBUTES"]
	if want := "deployment.environment.name=prod,k8s.cluster.name=west,department=eng,gen_ai.agent.id=team/a-kagent,gen_ai.agent.name=a-kagent,service.namespace=team"; attributes != want {
		t.Fatalf("resource attributes = %q, want %q", attributes, want)
	}
}

func TestTelemetryConfigFromProcessDisablesInvalidSignals(t *testing.T) {
	clearTelemetryEnvironment(t)
	t.Setenv("OTEL_TRACES_EXPORTER", "otlp")
	t.Setenv("OTEL_METRICS_EXPORTER", "prometheus")
	t.Setenv("OTEL_LOGS_EXPORTER", "otlp")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "not a URL")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "http://logs:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL", "http/json")
	t.Setenv("OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT", "true")

	got, warnings := translator.TelemetryConfigFromProcess()
	if got.Enabled() || got.CaptureSensitiveContent {
		t.Fatalf("invalid telemetry settings remain enabled: %#v", got)
	}
	messages := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		messages = append(messages, warning.Error())
	}
	joined := strings.Join(messages, "\n")
	for _, want := range []string{"traces endpoint", "OTEL_METRICS_EXPORTER", "logs protocol", "OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT"} {
		if !strings.Contains(joined, want) {
			t.Errorf("warnings %q do not mention %s", joined, want)
		}
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
			t.Setenv("OTEL_TRACES_EXPORTER", "otlp")
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", test.endpoint)
			t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", test.protocol)

			got, warnings := translator.TelemetryConfigFromProcess()
			if got.Traces.Enabled || len(warnings) != 1 {
				t.Fatalf("telemetry = %#v, warnings = %v", got, warnings)
			}
		})
	}
}

func TestTelemetryConfigFromProcessReportsInvalidOperatorSettings(t *testing.T) {
	clearTelemetryEnvironment(t)
	t.Setenv("OTEL_EXPORTER_OTLP_TIMEOUT", "15s")
	t.Setenv(translator.OperatorResourceAttributesVariable, "deployment.environment.name=prod,broken")

	got, warnings := translator.TelemetryConfigFromProcess()
	if len(warnings) != 2 || got.Timeout != "" || got.ResourceAttributes != "deployment.environment.name=prod" {
		t.Fatalf("telemetry = %#v, warnings = %v", got, warnings)
	}
}

func TestOwnsTelemetryEnvironment(t *testing.T) {
	for _, name := range []string{
		"OTEL_SDK_DISABLED",
		"OTEL_TRACES_EXPORTER",
		"OTEL_METRICS_EXPORTER",
		"OTEL_LOGS_EXPORTER",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_PROTOCOL",
		"OTEL_EXPORTER_OTLP_LOGS_PROTOCOL",
		"OTEL_SERVICE_NAME",
		"OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT",
	} {
		if !translator.OwnsTelemetryEnvironment(name) {
			t.Errorf("OwnsTelemetryEnvironment(%q) = false", name)
		}
	}
	for _, name := range []string{"OTEL_BSP_SCHEDULE_DELAY", "OTEL_EXPORTER_OTLP_HEADERS", "OTEL_TRACES_SAMPLER", "OTEL_RESOURCE_ATTRIBUTES", "OTEL_PROPAGATORS", "OTEL_SPAN_ATTRIBUTE_VALUE_LENGTH_LIMIT"} {
		if translator.OwnsTelemetryEnvironment(name) {
			t.Errorf("OwnsTelemetryEnvironment(%q) = true", name)
		}
	}
}

func clearTelemetryEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"OTEL_SDK_DISABLED",
		"OTEL_TRACES_EXPORTER",
		"OTEL_METRICS_EXPORTER",
		"OTEL_LOGS_EXPORTER",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_PROTOCOL",
		"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL",
		"OTEL_EXPORTER_OTLP_METRICS_PROTOCOL",
		"OTEL_EXPORTER_OTLP_LOGS_PROTOCOL",
		"OTEL_EXPORTER_OTLP_TIMEOUT",
		"OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT",
		"KAGENT_OTEL_RESOURCE_ATTRIBUTES",
		"KAGENT_OTEL_CAPTURE_RAW_API_BODIES",
		"KAGENT_OTEL_MAX_CAPTURE_BYTES",
		"OTEL_SPAN_ATTRIBUTE_VALUE_LENGTH_LIMIT",
	} {
		t.Setenv(name, "")
	}
}
