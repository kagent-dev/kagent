package telemetry

import (
	"context"
	"fmt"
	"os"

	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
)

const (
	ServiceName      = "kagent-controller"
	ServiceNamespace = "kagent"
)

// InitTracerProvider configures an OTLP TracerProvider and registers it as
// the global tracer. The exporter type and endpoint are read from standard
// OTEL environment variables (OTEL_EXPORTER_OTLP_PROTOCOL,
// OTEL_EXPORTER_OTLP_TRACES_ENDPOINT, OTEL_EXPORTER_OTLP_ENDPOINT, etc.).
// The returned shutdown function must be called on process exit to flush
// in-flight spans. If tracing is disabled a no-op is returned.
func InitTracerProvider(ctx context.Context, serviceVersion string) (func(context.Context) error, error) {
	if !env.OtelTracingEnabled.Get() {
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := autoexport.NewSpanExporter(ctx)
	if err != nil {
		return nil, fmt.Errorf("create span exporter: %w", err)
	}

	instanceID, err := os.Hostname()
	if err != nil || instanceID == "" {
		instanceID = uuid.New().String()
	}

	attrs := []attribute.KeyValue{
		semconv.ServiceName(ServiceName),
		semconv.ServiceVersion(serviceVersion),
		semconv.ServiceNamespace(ServiceNamespace),
		semconv.ServiceInstanceID(instanceID),
	}
	if ns := os.Getenv("KAGENT_NAMESPACE"); ns != "" {
		attrs = append(attrs, semconv.K8SNamespaceName(ns))
	}
	if pod := os.Getenv("K8S_POD_NAME"); pod != "" {
		attrs = append(attrs, semconv.K8SPodName(pod))
	}
	if node := os.Getenv("K8S_NODE_NAME"); node != "" {
		attrs = append(attrs, semconv.K8SNodeName(node))
	}

	res, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithAttributes(attrs...),
		resource.WithFromEnv(),
	)
	if err != nil {
		return nil, fmt.Errorf("create OTEL resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}
