package translator_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	corev1 "k8s.io/api/core/v1"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/kagent-dev/kagent/go/core/pkg/config"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

func TestRuntimeDefaultsIgnoreProcessEnvironment(t *testing.T) {
	cfg := &configv1.ControllerConfig{}
	config.DefaultController(cfg)
	cfg.RuntimeDefaults.Telemetry.Traces = &configv1.OTLPConfig{Endpoint: "http://collector:4317", Protocol: new("grpc"), Timeout: durationpb.New(time.Second)}
	first, err := translator.TelemetryConfigFromSettings(cfg.GetRuntimeDefaults().GetTelemetry())
	require.NoError(t, err)
	firstRuntime := translator.RuntimeConfig(cfg.GetRuntimeDefaults(), "assistant", "team")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://other:4317")
	t.Setenv("OTEL_SDK_DISABLED", "true")
	second, err := translator.TelemetryConfigFromSettings(cfg.GetRuntimeDefaults().GetTelemetry())
	require.NoError(t, err)
	require.Equal(t, first, second)
	secondRuntime := translator.RuntimeConfig(cfg.GetRuntimeDefaults(), "assistant", "team")
	require.True(t, proto.Equal(firstRuntime, secondRuntime))
	require.Equal(t, "assistant", secondRuntime.GetName())
	require.Equal(t, "team", secondRuntime.GetNamespace())
	require.Equal(t, "http://collector:4317", secondRuntime.GetTelemetry().GetTraces().GetEndpoint())
	require.Equal(t, []string{"http://collector:4317"}, first.Destinations(nil))
	require.Equal(t, int64(1000), first.Runtime.Exporters.Traces.TimeoutMillis)
}

func TestBYOTelemetryGenericEnvironmentOverridesFileDefaults(t *testing.T) {
	cfg := &configv1.ControllerConfig{}
	config.DefaultController(cfg)
	cfg.RuntimeDefaults.Telemetry.Traces = &configv1.OTLPConfig{Endpoint: "http://file-collector:4317", Protocol: new("grpc")}
	settings, err := translator.TelemetryConfigFromSettings(cfg.GetRuntimeDefaults().GetTelemetry())
	require.NoError(t, err)
	credential := corev1.EnvVar{Name: "OPENAI_API_KEY", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{Key: "api-key", LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}}}}
	environment := settings.TelemetryEnvironment(tracing.RuntimeTelemetry{}, []corev1.EnvVar{
		credential,
		{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: "http://runtime-collector:4318"},
		{Name: "OTEL_EXPORTER_OTLP_PROTOCOL", Value: "http/protobuf"},
	})
	require.Contains(t, environment, credential)
	for _, variable := range environment {
		require.NotEqual(t, "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", variable.Name)
	}
	destinations := settings.Destinations(environment)
	require.NotEmpty(t, destinations)
	for _, destination := range destinations {
		require.Equal(t, "http://runtime-collector:4318", destination)
	}
}

func TestBYOTelemetryServiceNameDefault(t *testing.T) {
	for _, tc := range []struct {
		name, serviceName, attributes, want string
	}{
		{name: "agent default", want: "assistant"},
		{name: "resource override", attributes: "service.name=custom", want: "custom"},
		{name: "image service override", serviceName: "image-service", attributes: "service.name=custom", want: "image-service"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OTEL_SERVICE_NAME", tc.serviceName)
			cfg := &configv1.ControllerConfig{}
			config.DefaultController(cfg)
			cfg.RuntimeDefaults.Telemetry.ResourceAttributes = map[string]string{"deployment.environment.name": "production"}
			settings, err := translator.TelemetryConfigFromSettings(cfg.GetRuntimeDefaults().GetTelemetry())
			require.NoError(t, err)
			environment := settings.TelemetryEnvironment(tracing.RuntimeTelemetry{AgentName: "assistant", AgentNamespace: "team"}, []corev1.EnvVar{{Name: "OTEL_RESOURCE_ATTRIBUTES", Value: tc.attributes}})
			for _, variable := range environment {
				if variable.Name == "OTEL_RESOURCE_ATTRIBUTES" {
					t.Setenv(variable.Name, variable.Value)
				}
			}
			// An ordinary SDK must recover the identity without kagent's JSON loader.
			res, err := resource.New(t.Context(), resource.WithFromEnv())
			require.NoError(t, err)
			name, ok := res.Set().Value(semconv.ServiceNameKey)
			require.True(t, ok)
			require.Equal(t, tc.want, name.AsString())
			require.Equal(t, map[string]string{"deployment.environment.name": "production"}, settings.Runtime.Exporters.ResourceAttributes)
		})
	}
}

func TestCompilerPreservesSharedRuntimeTelemetry(t *testing.T) {
	cfg := &configv1.TelemetryConfig{
		CaptureContent: true, CaptureRawApiBodies: true, OpenaiNativeTracing: true, MaxCaptureBytes: new(int32(4096)),
		ResourceAttributes: map[string]string{"deployment.environment.name": "test"},
		Traces:             &configv1.OTLPConfig{Endpoint: "http://collector:4317", Timeout: durationpb.New(time.Second)},
	}
	compiled, err := translator.TelemetryConfigFromSettings(cfg)
	require.NoError(t, err)
	want := config.RuntimeTelemetry(cfg)
	want.Runtime, want.AgentName, want.AgentNamespace = tracing.RuntimeADKGo, "assistant", "team"
	want.Provider, want.Model = "openai", "gpt-4"
	actual := compiled.RuntimeTelemetry(tracing.RuntimeADKGo, "assistant", "team", v1alpha3.ModelConfigSpec{Provider: v1alpha3.ModelProviderOpenAI, Model: "gpt-4"})
	require.Equal(t, want, actual)
}
