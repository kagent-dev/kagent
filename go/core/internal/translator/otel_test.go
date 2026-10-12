package translator_test

import (
	"testing"
	"time"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/kagent-dev/kagent/go/core/pkg/config"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
	corev1 "k8s.io/api/core/v1"
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
