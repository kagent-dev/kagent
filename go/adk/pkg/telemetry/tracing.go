package telemetry

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/pkg/tracing"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/trace"
	adktelemetry "google.golang.org/adk/v2/telemetry"
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

// PreResponseFlushEnabled reports whether spans must be exported before a turn's
// response leaves the process. Set through KAGENT_PRE_RESPONSE_TRACE_FLUSH, which the
// controller puts on Agent Substrate actors: a checkpoint/suspend runtime freezes as
// soon as the response is out, so the batch exporter's timer never fires for a
// session's last message. Everywhere else the timer suffices and a per-turn flush
// would only add export churn and, during a collector outage, response-tail latency.
func PreResponseFlushEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("KAGENT_PRE_RESPONSE_TRACE_FLUSH")), "true")
}

// ForceFlush exports any spans still buffered in the tracer provider's batch
// processor. The implementation lives in the shared tracing package so the
// non-ADK harnesses can use the same export boundary. Keep this wrapper for the
// ADK executor, which owns the pre-response invocation flush.
func ForceFlush(ctx context.Context) {
	if err := tracing.ForceFlush(ctx); err != nil {
		otel.Handle(err)
	}
}

// Init initializes OpenTelemetry providers for Go ADK, sets global providers and
// propagators, and returns a shutdown function.
func Init(ctx context.Context, serviceName string, serviceNamespace string) (shutdown func(context.Context) error, enabled bool, err error) {
	if !isTelemetryEnabled() {
		return func(context.Context) error { return nil }, false, nil
	}

	telemetryResource, err := tracing.NewResource(ctx, serviceName, serviceNamespace)
	if err != nil {
		return nil, true, err
	}

	tracingEnabled := strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_TRACING_ENABLED")), "true")
	loggingEnabled := strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_LOGGING_ENABLED")), "true")
	// Construct only explicitly enabled providers. adktelemetry.New creates
	// defaults for omitted providers, including signals disabled by our flags.
	telemetryProviders := &adktelemetry.Providers{}
	defer func() {
		if err != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			err = errors.Join(err, telemetryProviders.Shutdown(cleanupCtx))
		}
	}()
	if tracingEnabled {
		tracerProvider, tpErr := tracing.NewTracerProvider(ctx, telemetryResource, kagentAttributesSpanProcessor{})
		if tpErr != nil {
			return nil, true, tpErr
		}
		telemetryProviders.TracerProvider = tracerProvider
	}
	if loggingEnabled {
		loggerProvider, lpErr := newLoggerProvider(ctx, telemetryResource)
		if lpErr != nil {
			return nil, true, lpErr
		}
		telemetryProviders.LoggerProvider = loggerProvider
	}

	telemetryProviders.SetGlobalOtelProviders()
	tracing.SetPropagator()

	return telemetryProviders.Shutdown, true, nil
}

func isTelemetryEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_TRACING_ENABLED")), "true") ||
		strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_LOGGING_ENABLED")), "true")
}

func newLoggerProvider(ctx context.Context, res *resource.Resource) (*sdklog.LoggerProvider, error) {
	protocol := tracing.OTLPProtocol("LOGS")
	logEndpoint := tracing.OTLPEndpoint("LOGS")

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
