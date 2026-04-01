package telemetry

import (
	"context"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.36.0"
	"go.opentelemetry.io/otel/trace"
	adktelemetry "google.golang.org/adk/telemetry"
)

// SetKAgentSpanAttributes sets kagent span attributes in the OpenTelemetry context
func SetKAgentSpanAttributes(ctx context.Context, attributes map[string]string) context.Context {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		for key, value := range attributes {
			if value != "" {
				span.SetAttributes(attribute.String(key, value))
			}
		}
	}
	return ctx
}

// Init initializes OpenTelemetry providers for Go ADK, sets global providers and
// propagators, and returns a shutdown function.
func Init(ctx context.Context, serviceName string, serviceNamespace string) (shutdown func(context.Context) error, enabled bool, err error) {
	if !isTelemetryEnabled() {
		return func(context.Context) error { return nil }, false, nil
	}

	telemetryResource, err := resource.New(ctx, resource.WithAttributes(
		semconv.ServiceNameKey.String(serviceName),
		semconv.ServiceNamespaceKey.String(serviceNamespace),
	))
	if err != nil {
		return nil, true, err
	}

	tracingEnabled := strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_TRACING_ENABLED")), "true")
	loggingEnabled := strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_LOGGING_ENABLED")), "true")
	otelOpts := []adktelemetry.Option{adktelemetry.WithResource(telemetryResource)}
	if tracingEnabled {
		tracerProvider, tpErr := newGRPCTracerProvider(ctx, telemetryResource)
		if tpErr != nil {
			return nil, true, tpErr
		}
		otelOpts = append(otelOpts, adktelemetry.WithTracerProvider(tracerProvider))
	}
	if loggingEnabled {
		loggerProvider, lpErr := newGRPCLoggerProvider(ctx, telemetryResource)
		if lpErr != nil {
			return nil, true, lpErr
		}
		otelOpts = append(otelOpts, adktelemetry.WithLoggerProvider(loggerProvider))
	}

	telemetryProviders, telErr := adktelemetry.New(ctx, otelOpts...)
	if telErr != nil {
		return nil, true, telErr
	}

	telemetryProviders.SetGlobalOtelProviders()
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return telemetryProviders.Shutdown, true, nil
}

func isTelemetryEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_TRACING_ENABLED")), "true") ||
		strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_LOGGING_ENABLED")), "true")
}

func newGRPCTracerProvider(ctx context.Context, res *resource.Resource) (*sdktrace.TracerProvider, error) {
	traceEndpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"))
	if traceEndpoint == "" {
		traceEndpoint = strings.TrimSpace(os.Getenv("OTEL_TRACING_EXPORTER_OTLP_ENDPOINT"))
	}
	if traceEndpoint == "" {
		traceEndpoint = strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	}

	var opts []otlptracegrpc.Option
	if traceEndpoint != "" {
		opts = append(opts, otlptracegrpc.WithEndpointURL(traceEndpoint))
	}

	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, err
	}

	return sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	), nil
}

func newGRPCLoggerProvider(ctx context.Context, res *resource.Resource) (*sdklog.LoggerProvider, error) {
	logEndpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"))
	if logEndpoint == "" {
		logEndpoint = strings.TrimSpace(os.Getenv("OTEL_LOGGING_EXPORTER_OTLP_ENDPOINT"))
	}
	if logEndpoint == "" {
		logEndpoint = strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	}

	var opts []otlploggrpc.Option
	if logEndpoint != "" {
		opts = append(opts, otlploggrpc.WithEndpointURL(logEndpoint))
	}

	exporter, err := otlploggrpc.New(ctx, opts...)
	if err != nil {
		return nil, err
	}

	return sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
		sdklog.WithResource(res),
	), nil
}
