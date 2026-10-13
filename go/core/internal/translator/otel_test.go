package translator_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"google.golang.org/protobuf/types/known/durationpb"
	corev1 "k8s.io/api/core/v1"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/kagent-dev/kagent/go/core/pkg/config"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

func TestCompilerSettingsIgnoreProcessEnvironment(t *testing.T) {
	cfg := &apiv1.ControllerConfig{}
	config.DefaultController(cfg)
	cfg.RuntimeDefaults.Telemetry.Traces = &apiv1.OTLPConfig{Endpoint: "http://collector:4317", Protocol: new("grpc"), Timeout: durationpb.New(time.Second)}
	first, err := translator.NewCompilerSettings(cfg.RuntimeDefaults)
	require.NoError(t, err)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://other:4317")
	t.Setenv("OTEL_SDK_DISABLED", "true")
	second, err := translator.NewCompilerSettings(cfg.RuntimeDefaults)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, []string{"http://collector:4317"}, first.Telemetry.Destinations(nil))
	require.Equal(t, int64(1000), first.Telemetry.Exporters.Traces.TimeoutMillis)
}

func TestBYOTelemetryGenericEnvironmentOverridesFileDefaults(t *testing.T) {
	cfg := &apiv1.ControllerConfig{}
	config.DefaultController(cfg)
	cfg.RuntimeDefaults.Telemetry.Traces = &apiv1.OTLPConfig{Endpoint: "http://file-collector:4317", Protocol: new("grpc")}
	settings, err := translator.NewCompilerSettings(cfg.RuntimeDefaults)
	require.NoError(t, err)
	credential := corev1.EnvVar{Name: "OPENAI_API_KEY", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{Key: "api-key", LocalObjectReference: corev1.LocalObjectReference{Name: "model-auth"}}}}
	environment := settings.Telemetry.TelemetryEnvironment(tracing.RuntimeTelemetry{}, []corev1.EnvVar{
		credential,
		{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: "http://runtime-collector:4318"},
		{Name: "OTEL_EXPORTER_OTLP_PROTOCOL", Value: "http/protobuf"},
	})
	require.Contains(t, environment, credential)
	for _, variable := range environment {
		require.NotEqual(t, "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", variable.Name)
	}
	destinations := settings.Telemetry.Destinations(environment)
	require.NotEmpty(t, destinations)
	for _, destination := range destinations {
		require.Equal(t, "http://runtime-collector:4318", destination)
	}
}

func TestCompilerSettingsRejectInvalidCaptureAndEndpoints(t *testing.T) {
	for _, input := range []string{`runtimeDefaults: {telemetry: {maxCaptureBytes: -1}}`, `runtimeDefaults: {telemetry: {traces: {endpoint: 'file:///private'}}}`} {
		cfg := &apiv1.ControllerConfig{}
		require.NoError(t, config.Decode([]byte(input), cfg))
		config.DefaultController(cfg)
		_, err := translator.NewCompilerSettings(cfg.RuntimeDefaults)
		require.Error(t, err)
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
			cfg := &apiv1.ControllerConfig{}
			config.DefaultController(cfg)
			cfg.RuntimeDefaults.Telemetry.ResourceAttributes = map[string]string{"deployment.environment.name": "production"}
			settings, err := translator.NewCompilerSettings(cfg.RuntimeDefaults)
			require.NoError(t, err)
			environment := settings.Telemetry.TelemetryEnvironment(tracing.RuntimeTelemetry{AgentName: "assistant", AgentNamespace: "team"}, []corev1.EnvVar{{Name: "OTEL_RESOURCE_ATTRIBUTES", Value: tc.attributes}})
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
			require.Equal(t, map[string]string{"deployment.environment.name": "production"}, settings.Telemetry.Exporters.ResourceAttributes)
		})
	}
}
