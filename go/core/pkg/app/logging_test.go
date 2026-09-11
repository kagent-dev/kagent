package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/core/internal/telemetry"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/stretchr/testify/require"
	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	"google.golang.org/protobuf/proto"
	ctrl "sigs.k8s.io/controller-runtime"
)

func TestSetupLoggerStartup(t *testing.T) {
	if os.Getenv("KAGENT_TEST_LOGGER_CHILD") == "true" {
		testLoggerStartup(t)
		return
	}
	for _, gate := range []string{"", "true"} {
		t.Run("gate="+gate, func(t *testing.T) {
			t.Setenv("OTEL_LOGGING_ENABLED", gate)
			t.Setenv("LOG_LEVEL", "info")
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSetupLoggerStartup$")
			command.Env = append(os.Environ(), "KAGENT_TEST_LOGGER_CHILD=true")
			var stderr, stdout bytes.Buffer
			command.Stderr, command.Stdout = &stderr, &stdout
			require.NoError(t, command.Run(), "stdout: %s; stderr: %s", stdout.String(), stderr.String())
			var messages []string
			scanner := bufio.NewScanner(&stderr)
			for scanner.Scan() {
				var record struct {
					Level     string `json:"level"`
					Message   string `json:"msg"`
					Component string `json:"component"`
				}
				require.NoError(t, json.Unmarshal(scanner.Bytes(), &record))
				require.Equal(t, "INFO", record.Level)
				require.Equal(t, "controller", record.Component)
				messages = append(messages, record.Message)
			}
			require.NoError(t, scanner.Err())
			require.ElementsMatch(t, []string{"startup logger", "runtime logger", "context logger"}, messages)
		})
	}
}

func TestRunValidatesLogLevelBeforeTelemetry(t *testing.T) {
	t.Setenv("LOG_LEVEL", "invalid")
	t.Setenv("OTEL_TRACING_ENABLED", "true")
	t.Setenv("OTEL_TRACES_EXPORTER", "invalid")
	require.ErrorContains(t, Run(t.Context(), Options{}), "parse LOG_LEVEL")
}

func testLoggerStartup(t *testing.T) {
	t.Helper()
	requests := make(chan []byte, 4)
	collector := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil || request.URL.Path != "/v1/logs" {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- body
		writer.Header().Set("Content-Type", "application/x-protobuf")
	}))
	t.Cleanup(collector.Close)
	t.Setenv("OTEL_LOGS_EXPORTER", "otlp")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL", "http/protobuf")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", collector.URL+"/v1/logs")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
	require.NoError(t, SetupLogger())
	startupLogger := slog.Default()
	require.NoError(t, SetupLogger())
	shutdown, err := telemetry.InitLoggerProvider(t.Context(), "test")
	require.NoError(t, err)
	startupLogger.DebugContext(t.Context(), "filtered")
	startupLogger.InfoContext(t.Context(), "startup logger", "component", "controller")
	ctrl.Log.Info("runtime logger", "component", "controller")
	ctx := logging.IntoContext(t.Context(), slog.Default())
	logging.FromContext(ctx).InfoContext(ctx, "context logger", "component", "controller")
	require.NoError(t, shutdown(t.Context()))
	var messages []string
	for len(requests) > 0 {
		var request collectorlogsv1.ExportLogsServiceRequest
		require.NoError(t, proto.Unmarshal(<-requests, &request))
		for _, resource := range request.ResourceLogs {
			for _, scope := range resource.ScopeLogs {
				for _, record := range scope.LogRecords {
					messages = append(messages, record.Body.GetStringValue())
				}
			}
		}
	}
	if os.Getenv("OTEL_LOGGING_ENABLED") == "true" {
		require.ElementsMatch(t, []string{"startup logger", "runtime logger", "context logger"}, messages)
	} else {
		require.Empty(t, messages)
	}
}
