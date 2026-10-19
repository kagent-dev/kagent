package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/pkg/config"
)

func TestTelemetryStandaloneDefaults(t *testing.T) {
	settings := config.Telemetry(&configv1.TelemetryConfig{
		Traces: &configv1.OTLPConfig{Endpoint: "http://collector:4317"},
	})
	require.Equal(t, "grpc", settings.Traces.Protocol)
	require.Nil(t, settings.Metrics)
	require.Nil(t, settings.Logs)
}
