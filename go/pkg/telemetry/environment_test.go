package telemetry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvironmentConnectionOverridesReplaceFilePair(t *testing.T) {
	for _, tc := range []struct {
		name        string
		environment []string
		want        OTLPConfig
	}{
		{name: "file", want: OTLPConfig{Endpoint: "http://file:4318/custom/traces", Protocol: "http/protobuf"}},
		{name: "generic endpoint", environment: []string{"OTEL_EXPORTER_OTLP_ENDPOINT=http://override:4317"}, want: OTLPConfig{Endpoint: "http://override:4317", Protocol: "grpc"}},
		{name: "specific endpoint", environment: []string{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://override:4317"}, want: OTLPConfig{Endpoint: "http://override:4317", Protocol: "grpc"}},
		{name: "generic protocol", environment: []string{"OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf"}, want: OTLPConfig{Endpoint: "http://localhost:4318/v1/traces", Protocol: "http/protobuf"}},
		{name: "specific protocol", environment: []string{"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=grpc"}, want: OTLPConfig{Endpoint: "http://localhost:4317", Protocol: "grpc"}},
		{name: "generic pair", environment: []string{"OTEL_EXPORTER_OTLP_ENDPOINT=http://override:4318/prefix", "OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf"}, want: OTLPConfig{Endpoint: "http://override:4318/prefix/v1/traces", Protocol: "http/protobuf"}},
		{name: "specific pair wins", environment: []string{"OTEL_EXPORTER_OTLP_ENDPOINT=http://generic:4318", "OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://specific:4317", "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=grpc"}, want: OTLPConfig{Endpoint: "http://specific:4317", Protocol: "grpc"}},
		{name: "mixed env scopes", environment: []string{"OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://specific:4318/custom"}, want: OTLPConfig{Endpoint: "http://specific:4318/custom", Protocol: "http/protobuf"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{Traces: &OTLPConfig{Endpoint: "http://file:4318/custom/traces", Protocol: "http/protobuf"}}
			got := Environment(tc.environment, cfg)
			require.Equal(t, &tc.want, OTLPExporter(got, "TRACES"))
			for _, explicit := range tc.environment {
				require.Contains(t, got, explicit)
			}
		})
	}
}

func TestEnvironmentCompactsOnlySharedGRPCConnections(t *testing.T) {
	for _, tc := range []struct {
		name        string
		metrics     *OTLPConfig
		environment []string
		wantShared  bool
	}{
		{name: "all shared", metrics: &OTLPConfig{Endpoint: "http://collector:4317", Protocol: "grpc"}, wantShared: true},
		{name: "disabled signal"},
		{name: "different endpoint", metrics: &OTLPConfig{Endpoint: "http://metrics:4317", Protocol: "grpc"}},
		{name: "different transport", metrics: &OTLPConfig{Endpoint: "http://metrics:4318/v1/metrics", Protocol: "http/protobuf"}},
		{name: "specific override", metrics: &OTLPConfig{Endpoint: "http://collector:4317", Protocol: "grpc"}, environment: []string{"OTEL_EXPORTER_OTLP_METRICS_PROTOCOL=http/protobuf"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{Traces: &OTLPConfig{Endpoint: "http://collector:4317", Protocol: "grpc"}, Metrics: tc.metrics, Logs: &OTLPConfig{Endpoint: "http://collector:4317", Protocol: "grpc"}}
			got := Environment(tc.environment, cfg)
			require.Equal(t, &OTLPConfig{Endpoint: "http://collector:4317", Protocol: "grpc"}, OTLPExporter(got, "TRACES"))
			require.Equal(t, &OTLPConfig{Endpoint: "http://collector:4317", Protocol: "grpc"}, OTLPExporter(got, "LOGS"))
			switch {
			case tc.metrics == nil:
				require.Nil(t, OTLPExporter(got, "METRICS"))
			case len(tc.environment) > 0:
				require.Equal(t, &OTLPConfig{Endpoint: "http://localhost:4318/v1/metrics", Protocol: "http/protobuf"}, OTLPExporter(got, "METRICS"))
			default:
				require.Equal(t, tc.metrics, OTLPExporter(got, "METRICS"))
			}
			if tc.wantShared {
				require.Len(t, got, 8)
				require.Contains(t, got, "OTEL_EXPORTER_OTLP_ENDPOINT=http://collector:4317")
			} else {
				require.NotContains(t, got, "OTEL_EXPORTER_OTLP_ENDPOINT=http://collector:4317")
			}
		})
	}
}

func TestEnvironmentTimeoutDefaultsAndOverrides(t *testing.T) {
	for _, tc := range []struct {
		name        string
		millis      int64
		environment []string
		want        string
	}{
		{name: "implicit SDK default"},
		{name: "explicit SDK default", millis: 10000},
		{name: "file", millis: 1234, want: "OTEL_EXPORTER_OTLP_TRACES_TIMEOUT=1234"},
		{name: "generic env", millis: 1234, environment: []string{"OTEL_EXPORTER_OTLP_TIMEOUT=2345"}},
		{name: "specific env", millis: 1234, environment: []string{"OTEL_EXPORTER_OTLP_TRACES_TIMEOUT=3456"}, want: "OTEL_EXPORTER_OTLP_TRACES_TIMEOUT=3456"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Environment(tc.environment, Config{Traces: &OTLPConfig{Endpoint: "http://collector:4317", Protocol: "grpc", TimeoutMillis: tc.millis}})
			for _, explicit := range tc.environment {
				require.Contains(t, got, explicit)
			}
			if tc.want != "" {
				require.Contains(t, got, tc.want)
			} else {
				require.NotContains(t, got, "OTEL_EXPORTER_OTLP_TRACES_TIMEOUT=10000")
				require.NotContains(t, got, "OTEL_EXPORTER_OTLP_TRACES_TIMEOUT=1234")
			}
		})
	}
}
