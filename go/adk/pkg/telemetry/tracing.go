package telemetry

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
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
	merged := mergeAttributes(contextAttributes(ctx), attributes)
	setSpanAttributes(ctx, stringAttributes(merged)...)
	if len(merged) == 0 {
		return ctx
	}
	return context.WithValue(ctx, kagentSpanAttributesKey{}, merged)
}

// StartInvocationSpan creates a lightweight root span around one executor run.
// Descendant spans inherit request-scoped attributes via the span processor.
func StartInvocationSpan(ctx context.Context) (context.Context, trace.Span) {
	return otel.Tracer("gcp.vertex.agent").Start(ctx, "invocation")
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
		tracerProvider, tpErr := newTracerProvider(ctx, telemetryResource)
		if tpErr != nil {
			return nil, true, tpErr
		}
		otelOpts = append(otelOpts, adktelemetry.WithTracerProvider(tracerProvider))
	}
	if loggingEnabled {
		loggerProvider, lpErr := newLoggerProvider(ctx, telemetryResource)
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

// resolveOTLPProtocol returns the OTLP protocol for the given signal,
// following OTel spec precedence: signal-specific > general > default (grpc).
func resolveOTLPProtocol(signal string) string {
	if v := strings.TrimSpace(os.Getenv(fmt.Sprintf("OTEL_EXPORTER_OTLP_%s_PROTOCOL", signal))); v != "" {
		return strings.ToLower(v)
	}
	if v := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")); v != "" {
		return strings.ToLower(v)
	}
	return "grpc"
}

func resolveEndpoint(signalEnvSuffix string) string {
	endpoint := strings.TrimSpace(os.Getenv(fmt.Sprintf("OTEL_EXPORTER_OTLP_%s_ENDPOINT", signalEnvSuffix)))
	if endpoint == "" {
		endpoint = strings.TrimSpace(os.Getenv(fmt.Sprintf("OTEL_%s_EXPORTER_OTLP_ENDPOINT", signalEnvSuffix)))
	}
	if endpoint == "" {
		endpoint = strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	}
	return endpoint
}

func newTracerProvider(ctx context.Context, res *resource.Resource) (*sdktrace.TracerProvider, error) {
	protocol := resolveOTLPProtocol("TRACES")
	traceEndpoint := resolveEndpoint("TRACES")

	var exporter sdktrace.SpanExporter
	var err error

	switch protocol {
	case "http/protobuf":
		var opts []otlptracehttp.Option
		if traceEndpoint != "" {
			opts = append(opts, otlptracehttp.WithEndpointURL(traceEndpoint))
		}
		opts = append(opts, otlptracehttp.WithRetry(otlptracehttp.RetryConfig{
			Enabled:         true,
			InitialInterval: 1 * time.Second,
			MaxInterval:     5 * time.Second,
			MaxElapsedTime:  30 * time.Second,
		}))
		exporter, err = otlptracehttp.New(ctx, opts...)
	default:
		var opts []otlptracegrpc.Option
		opts = append(opts, otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{
			Enabled:         true,
			InitialInterval: 1 * time.Second,
			MaxInterval:     5 * time.Second,
			MaxElapsedTime:  30 * time.Second,
		}))
		if traceEndpoint != "" {
			if u, parseErr := url.Parse(traceEndpoint); parseErr == nil && u.Scheme != "" && u.Host != "" {
				opts = append(opts, otlptracegrpc.WithEndpointURL(u.String()))
			} else {
				opts = append(opts, otlptracegrpc.WithEndpoint(traceEndpoint))
			}
		}
		exporter, err = otlptracegrpc.New(ctx, opts...)
	}
	if err != nil {
		return nil, err
	}

	return sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(kagentAttributesSpanProcessor{}),
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	), nil
}

func newLoggerProvider(ctx context.Context, res *resource.Resource) (*sdklog.LoggerProvider, error) {
	protocol := resolveOTLPProtocol("LOGS")
	logEndpoint := resolveEndpoint("LOGS")

	var exporter sdklog.Exporter
	var err error

	switch protocol {
	case "http/protobuf":
		var opts []otlploghttp.Option
		if logEndpoint != "" {
			opts = append(opts, otlploghttp.WithEndpointURL(logEndpoint))
		}
		exporter, err = otlploghttp.New(ctx, opts...)
	default:
		var opts []otlploggrpc.Option
		if logEndpoint != "" {
			if u, parseErr := url.Parse(logEndpoint); parseErr == nil && u.Scheme != "" && u.Host != "" {
				opts = append(opts, otlploggrpc.WithEndpointURL(u.String()))
			} else {
				opts = append(opts, otlploggrpc.WithEndpoint(logEndpoint))
			}
		}
		exporter, err = otlploggrpc.New(ctx, opts...)
	}
	if err != nil {
		return nil, err
	}

	return sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
		sdklog.WithResource(res),
	), nil
}
