package telemetry

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitTelemetry_Disabled(t *testing.T) {
	// Ensure OTEL_TRACING_ENABLED is not set to "true"
	originalValue := os.Getenv("OTEL_TRACING_ENABLED")
	defer os.Setenv("OTEL_TRACING_ENABLED", originalValue)

	os.Setenv("OTEL_TRACING_ENABLED", "false")

	ctx := context.Background()
	shutdown, err := InitTelemetry(ctx)

	assert.NoError(t, err)
	assert.NotNil(t, shutdown)

	// Should be safe to call shutdown multiple times
	shutdown()
	shutdown()
}

func TestInitTelemetry_Enabled(t *testing.T) {
	// Skip this test if we don't have OTLP endpoint available
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		t.Skip("Skipping OTEL integration test - no OTLP endpoint configured")
	}

	originalValue := os.Getenv("OTEL_TRACING_ENABLED")
	defer os.Setenv("OTEL_TRACING_ENABLED", originalValue)

	os.Setenv("OTEL_TRACING_ENABLED", "true")

	ctx := context.Background()
	shutdown, err := InitTelemetry(ctx)

	assert.NoError(t, err)
	assert.NotNil(t, shutdown)
	assert.NotNil(t, tracer)
	assert.NotNil(t, meter)

	// Cleanup
	shutdown()
}

func TestInstrumentedCommandExecution_Success(t *testing.T) {
	// Setup telemetry in disabled mode for testing
	originalValue := os.Getenv("OTEL_TRACING_ENABLED")
	defer os.Setenv("OTEL_TRACING_ENABLED", originalValue)

	os.Setenv("OTEL_TRACING_ENABLED", "false")

	ctx := context.Background()
	command := "echo"
	args := []string{"hello", "world"}
	caller := "test"
	expectedOutput := "hello world"

	execFunc := func() (string, error) {
		return expectedOutput, nil
	}

	output, err := InstrumentedCommandExecution(ctx, command, args, caller, execFunc)

	assert.NoError(t, err)
	assert.Equal(t, expectedOutput, output)
}

func TestInstrumentedCommandExecution_Error(t *testing.T) {
	originalValue := os.Getenv("OTEL_TRACING_ENABLED")
	defer os.Setenv("OTEL_TRACING_ENABLED", originalValue)

	os.Setenv("OTEL_TRACING_ENABLED", "false")

	ctx := context.Background()
	command := "failing-command"
	args := []string{"arg1"}
	caller := "test"
	expectedError := errors.New("command failed")

	execFunc := func() (string, error) {
		return "", expectedError
	}

	output, err := InstrumentedCommandExecution(ctx, command, args, caller, execFunc)

	assert.Error(t, err)
	assert.Equal(t, expectedError, err)
	assert.Empty(t, output)
}

func TestInstrumentedCommandExecution_WithTracing(t *testing.T) {
	// Skip this test if we don't have OTLP endpoint available
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		t.Skip("Skipping OTEL integration test - no OTLP endpoint configured")
	}

	originalValue := os.Getenv("OTEL_TRACING_ENABLED")
	defer os.Setenv("OTEL_TRACING_ENABLED", originalValue)

	os.Setenv("OTEL_TRACING_ENABLED", "true")

	ctx := context.Background()
	shutdown, err := InitTelemetry(ctx)
	require.NoError(t, err)
	defer shutdown()

	command := "test-command"
	args := []string{"arg1", "arg2"}
	caller := "test_func"
	expectedOutput := "test output"

	execFunc := func() (string, error) {
		// Simulate some work
		time.Sleep(10 * time.Millisecond)
		return expectedOutput, nil
	}

	output, err := InstrumentedCommandExecution(ctx, command, args, caller, execFunc)

	assert.NoError(t, err)
	assert.Equal(t, expectedOutput, output)
}

func TestGetTracer(t *testing.T) {
	// Initially, tracer should be nil
	tracer = nil
	assert.Nil(t, GetTracer())

	// After initialization, tracer should be available
	originalValue := os.Getenv("OTEL_TRACING_ENABLED")
	defer os.Setenv("OTEL_TRACING_ENABLED", originalValue)

	os.Setenv("OTEL_TRACING_ENABLED", "false")

	// Even when disabled, the global tracer might be available if set elsewhere
	ctx := context.Background()
	shutdown, err := InitTelemetry(ctx)
	require.NoError(t, err)
	defer shutdown()

	// For disabled mode, tracer might still be nil
	// This is expected behavior
}

func TestGetMeter(t *testing.T) {
	// Initially, meter should be nil
	meter = nil
	assert.Nil(t, GetMeter())

	originalValue := os.Getenv("OTEL_TRACING_ENABLED")
	defer os.Setenv("OTEL_TRACING_ENABLED", originalValue)

	os.Setenv("OTEL_TRACING_ENABLED", "false")

	ctx := context.Background()
	shutdown, err := InitTelemetry(ctx)
	require.NoError(t, err)
	defer shutdown()

	// For disabled mode, meter might still be nil
	// This is expected behavior
}
