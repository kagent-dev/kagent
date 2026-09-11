// Package tracing initializes process-wide OpenTelemetry tracing for harness runtimes.
package tracing

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.36.0"
)

// Init configures the global tracer provider and W3C propagator from the
// standard OTEL environment. It is intended to be called once by a process
// entrypoint. KAGENT_NAME identifies the service when set; fallbackServiceName
// supports standalone harness validation where the compiler-owned environment
// may be absent.
func Init(ctx context.Context, fallbackServiceName string) (func(context.Context) error, bool, error) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_TRACING_ENABLED")), "true") {
		return func(context.Context) error { return nil }, false, nil
	}
	serviceName := environmentValue("KAGENT_NAME", fallbackServiceName)
	serviceNamespace := environmentValue("KAGENT_NAMESPACE", "default")

	res, err := NewResource(ctx, serviceName, serviceNamespace)
	if err != nil {
		return nil, true, err
	}
	provider, err := NewTracerProvider(ctx, res)
	if err != nil {
		return nil, true, err
	}
	otel.SetTracerProvider(provider)
	SetPropagator()
	return provider.Shutdown, true, nil
}

// NewResource combines service identity, SDK metadata, and OTEL resource attributes.
func NewResource(ctx context.Context, serviceName, serviceNamespace string) (*resource.Resource, error) {
	return resource.New(ctx, resource.WithFromEnv(), resource.WithTelemetrySDK(), resource.WithAttributes(
		semconv.ServiceNameKey.String(serviceName), semconv.ServiceNamespaceKey.String(serviceNamespace)))
}

// NewTracerProvider creates a batched OTLP provider without installing it globally.
// Additional processors let harnesses enrich spans without duplicating exporter setup.
func NewTracerProvider(ctx context.Context, res *resource.Resource, processors ...sdktrace.SpanProcessor) (*sdktrace.TracerProvider, error) {
	exporter, err := newExporter(ctx)
	if err != nil {
		return nil, err
	}
	opts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}
	for _, processor := range processors {
		opts = append(opts, sdktrace.WithSpanProcessor(processor))
	}
	opts = append(opts, sdktrace.WithBatcher(exporter))
	return sdktrace.NewTracerProvider(opts...), nil
}

// SetPropagator installs W3C trace context and baggage propagation.
func SetPropagator() {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
}

// OTLPProtocol resolves signal-specific configuration before the general setting.
func OTLPProtocol(signal string) string {
	return strings.ToLower(environmentValue("OTEL_EXPORTER_OTLP_"+signal+"_PROTOCOL", environmentValue("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")))
}

// OTLPEndpoint resolves signal-specific configuration, the legacy signal alias,
// then the general endpoint. An empty result leaves the exporter default in effect.
func OTLPEndpoint(signal string) string {
	return environmentValue("OTEL_EXPORTER_OTLP_"+signal+"_ENDPOINT",
		environmentValue("OTEL_"+signal+"_EXPORTER_OTLP_ENDPOINT", environmentValue("OTEL_EXPORTER_OTLP_ENDPOINT", "")))
}

func environmentValue(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

// ForceFlush exports spans buffered by the global tracer provider.
func ForceFlush(ctx context.Context) error {
	type flusher interface{ ForceFlush(context.Context) error }
	provider, ok := otel.GetTracerProvider().(flusher)
	if !ok {
		return nil
	}
	flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), flushTimeout())
	defer cancel()
	return provider.ForceFlush(flushCtx)
}

func flushTimeout() time.Duration {
	if value := strings.TrimSpace(os.Getenv("KAGENT_TRACE_FLUSH_TIMEOUT_MS")); value != "" {
		if milliseconds, err := strconv.Atoi(value); err == nil && milliseconds > 0 {
			return time.Duration(milliseconds) * time.Millisecond
		}
	}
	return 3 * time.Second
}

func newExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	endpoint := OTLPEndpoint("TRACES")
	protocol := OTLPProtocol("TRACES")

	switch protocol {
	case "grpc":
		opts := []otlptracegrpc.Option{otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{
			Enabled: true, InitialInterval: time.Second, MaxInterval: 5 * time.Second, MaxElapsedTime: 30 * time.Second,
		})}
		if endpoint != "" {
			if parsed, err := url.Parse(endpoint); err == nil && parsed.Scheme != "" && parsed.Host != "" {
				opts = append(opts, otlptracegrpc.WithEndpointURL(parsed.String()))
			} else {
				opts = append(opts, otlptracegrpc.WithEndpoint(endpoint))
			}
		}
		return otlptracegrpc.New(ctx, opts...)
	case "http/protobuf":
		opts := []otlptracehttp.Option{otlptracehttp.WithRetry(otlptracehttp.RetryConfig{
			Enabled: true, InitialInterval: time.Second, MaxInterval: 5 * time.Second, MaxElapsedTime: 30 * time.Second,
		})}
		if endpoint != "" {
			opts = append(opts, otlptracehttp.WithEndpointURL(endpoint))
		}
		return otlptracehttp.New(ctx, opts...)
	default:
		return nil, fmt.Errorf("unsupported OTLP trace protocol %q", protocol)
	}
}
