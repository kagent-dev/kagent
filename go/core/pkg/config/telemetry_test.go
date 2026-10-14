package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/pkg/config"
)

func TestTelemetryStandaloneDefaults(t *testing.T) {
	settings := config.Telemetry(&apiv1.TelemetryConfig{
		Traces: &apiv1.OTLPConfig{Endpoint: "http://collector:4317"},
	})
	require.Equal(t, "grpc", settings.Traces.Protocol)
	require.Nil(t, settings.Metrics)
	require.Nil(t, settings.Logs)
}
