package telemetry

import (
	"context"
	"os"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	serviceName    = "kagent-tools"
	serviceVersion = "1.0.0"
)

var (
	tracer trace.Tracer
	meter  metric.Meter

	// Metrics
	commandExecutionCounter  metric.Int64Counter
	commandExecutionDuration metric.Float64Histogram
	commandExecutionErrors   metric.Int64Counter
	activeCommandsGauge      metric.Int64UpDownCounter
)

// InitTelemetry initializes OpenTelemetry tracing and metrics
func InitTelemetry(ctx context.Context) (func(), error) {
	// Check if tracing is enabled
	tracingEnabled := os.Getenv("OTEL_TRACING_ENABLED")
	if tracingEnabled != "true" {
		// Return no-op shutdown function if tracing is disabled
		return func() {}, nil
	}

	// Create resource with service information
	resource := sdkresource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String(serviceName),
		semconv.ServiceVersionKey.String(serviceVersion),
	)

	// Set up trace exporter
	traceExporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, err
	}

	// Create trace provider
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(resource),
	)

	// Set up meter provider (for metrics)
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(resource),
	)

	// Set global providers
	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Initialize tracer and meter
	tracer = otel.Tracer(serviceName)
	meter = otel.Meter(serviceName)

	// Initialize metrics
	if err := initMetrics(); err != nil {
		return nil, err
	}

	// Return shutdown function
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tracerProvider.Shutdown(ctx)
		meterProvider.Shutdown(ctx)
	}, nil
}

func initMetrics() error {
	var err error

	// Command execution counter
	commandExecutionCounter, err = meter.Int64Counter(
		"command_executions_total",
		metric.WithDescription("Total number of command executions"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return err
	}

	// Command execution duration histogram
	commandExecutionDuration, err = meter.Float64Histogram(
		"command_execution_duration_seconds",
		metric.WithDescription("Duration of command executions"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return err
	}

	// Command execution errors counter
	commandExecutionErrors, err = meter.Int64Counter(
		"command_execution_errors_total",
		metric.WithDescription("Total number of command execution errors"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return err
	}

	// Active commands gauge
	activeCommandsGauge, err = meter.Int64UpDownCounter(
		"active_commands",
		metric.WithDescription("Number of currently active command executions"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return err
	}

	return nil
}

// InstrumentedCommandExecution provides tracing and metrics for command execution
func InstrumentedCommandExecution(ctx context.Context, command string, args []string, caller string, execFunc func() (string, error)) (string, error) {
	// Skip instrumentation if tracer is not initialized
	if tracer == nil {
		return execFunc()
	}

	// Create span for command execution
	spanName := "exec_command"
	ctx, span := tracer.Start(ctx, spanName)
	defer span.End()

	// Set span attributes
	span.SetAttributes(
		attribute.String("command.name", command),
		attribute.StringSlice("command.args", args),
		attribute.String("command.caller", caller),
		attribute.String("service.name", serviceName),
	)

	// Record metrics
	start := time.Now()

	// Increment active commands
	if activeCommandsGauge != nil {
		activeCommandsGauge.Add(ctx, 1, metric.WithAttributes(
			attribute.String("command", command),
		))
		defer activeCommandsGauge.Add(ctx, -1, metric.WithAttributes(
			attribute.String("command", command),
		))
	}

	// Execute the command
	output, err := execFunc()
	duration := time.Since(start)

	// Set span status and record additional attributes
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.SetAttributes(
			attribute.String("error.message", err.Error()),
			attribute.Bool("command.success", false),
		)

		// Record error metric
		if commandExecutionErrors != nil {
			commandExecutionErrors.Add(ctx, 1, metric.WithAttributes(
				attribute.String("command", command),
				attribute.String("error", err.Error()),
			))
		}
	} else {
		span.SetStatus(codes.Ok, "Command executed successfully")
		span.SetAttributes(
			attribute.Bool("command.success", true),
			attribute.Int("output.length", len(output)),
		)
	}

	span.SetAttributes(
		attribute.Float64("command.duration_seconds", duration.Seconds()),
	)

	// Record metrics
	if commandExecutionCounter != nil {
		commandExecutionCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("command", command),
			attribute.String("success", strconv.FormatBool(err == nil)),
		))
	}

	if commandExecutionDuration != nil {
		commandExecutionDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(
			attribute.String("command", command),
			attribute.String("success", strconv.FormatBool(err == nil)),
		))
	}

	return output, err
}

// GetTracer returns the global tracer instance
func GetTracer() trace.Tracer {
	return tracer
}

// GetMeter returns the global meter instance
func GetMeter() metric.Meter {
	return meter
}
