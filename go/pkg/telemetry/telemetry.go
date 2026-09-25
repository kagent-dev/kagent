// Package telemetry boots the OpenTelemetry SDK from the SDK-spec environment.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/contrib/propagators/autoprop"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/kagent-dev/kagent/go/pkg/telemetry/conv"
)

// FlushTimeout bounds one ForceFlush.
const FlushTimeout = 3 * time.Second

// Default is an SDK setting kagent applies when the environment leaves it unset.
type Default struct{ Name, Value string }

// Defaults keep baggage from leaking to tools and model providers, compress
// exports, and give every runtime the same histogram buckets.
var Defaults = []Default{
	{Name: "OTEL_PROPAGATORS", Value: "tracecontext"},
	{Name: "OTEL_EXPORTER_OTLP_COMPRESSION", Value: "gzip"},
	{Name: "OTEL_EXPORTER_OTLP_METRICS_DEFAULT_HISTOGRAM_AGGREGATION", Value: "base2_exponential_bucket_histogram"},
}

// WithDefaults adds each default that a child process environment leaves unset.
func WithDefaults(environment []string) []string {
	result := slices.Clone(environment)
	for _, value := range Defaults {
		if !slices.ContainsFunc(environment, func(variable string) bool {
			current, found := strings.CutPrefix(variable, value.Name+"=")
			return found && current != ""
		}) {
			result = append(result, value.Name+"="+value.Value)
		}
	}
	return result
}

// Options identify the process. The environment wins over Defaults.
// MetricReaders are added beside the reader the environment configures.
type Options struct {
	Runtime        string
	Defaults       []attribute.KeyValue
	SpanProcessors []sdktrace.SpanProcessor
	MetricReaders  []sdkmetric.Reader
}

// Providers holds the SDK providers Init installed, nil for a disabled signal.
type Providers struct {
	tracer *sdktrace.TracerProvider
	meter  *sdkmetric.MeterProvider
	logger *sdklog.LoggerProvider
}

// Init applies Defaults, then installs the global propagator and one provider
// per enabled signal.
// A signal that fails to configure stays off and is reported in the error.
func Init(ctx context.Context, opts Options) (*Providers, error) {
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		slog.Default().ErrorContext(context.Background(), "opentelemetry", "error", err)
	}))
	for _, value := range Defaults {
		if os.Getenv(value.Name) == "" {
			_ = os.Setenv(value.Name, value.Value)
		}
	}
	otel.SetTextMapPropagator(autoprop.NewTextMapPropagator())
	providers := &Providers{}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_SDK_DISABLED")), "true") {
		return providers, nil
	}
	var errs []error
	res, err := newResource(ctx, opts)
	if err != nil {
		if !errors.Is(err, resource.ErrPartialResource) {
			return providers, fmt.Errorf("telemetry resource: %w", err)
		}
		errs = append(errs, fmt.Errorf("telemetry resource: %w", err))
	}
	if exporter, err := autoexport.NewSpanExporter(ctx); err != nil {
		errs = append(errs, fmt.Errorf("traces: %w", err))
	} else if !autoexport.IsNoneSpanExporter(exporter) {
		options := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}
		for _, processor := range opts.SpanProcessors {
			options = append(options, sdktrace.WithSpanProcessor(processor))
		}
		providers.tracer = sdktrace.NewTracerProvider(append(options, sdktrace.WithBatcher(exporter))...)
		otel.SetTracerProvider(providers.tracer)
	}
	readers := slices.Clone(opts.MetricReaders)
	if reader, err := autoexport.NewMetricReader(ctx); err != nil {
		errs = append(errs, fmt.Errorf("metrics: %w", err))
	} else if !autoexport.IsNoneMetricReader(reader) {
		readers = append(readers, reader)
	}
	if len(readers) > 0 {
		options := []sdkmetric.Option{sdkmetric.WithResource(res)}
		for _, reader := range readers {
			options = append(options, sdkmetric.WithReader(reader))
		}
		providers.meter = sdkmetric.NewMeterProvider(options...)
		otel.SetMeterProvider(providers.meter)
	}
	if exporter, err := autoexport.NewLogExporter(ctx); err != nil {
		errs = append(errs, fmt.Errorf("logs: %w", err))
	} else if !autoexport.IsNoneLogExporter(exporter) {
		providers.logger = sdklog.NewLoggerProvider(sdklog.WithResource(res), sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)))
		global.SetLoggerProvider(providers.logger)
	}
	return providers, errors.Join(errs...)
}

func newResource(ctx context.Context, opts Options) (*resource.Resource, error) {
	detectors := []resource.Option{
		resource.WithAttributes(opts.Defaults...),
		resource.WithTelemetrySDK(),
		resource.WithFromEnv(),
	}
	if opts.Runtime != "" {
		detectors = append(detectors, resource.WithAttributes(conv.KagentRuntimeKey.String(opts.Runtime)))
	}
	return resource.New(ctx, detectors...)
}

// TracesEnabled reports whether spans are exported.
func (p *Providers) TracesEnabled() bool {
	return p != nil && p.tracer != nil
}

type flushRecordKey struct{}

// WithFlushRecord marks ctx as one request. Once a ForceFlush under it fails,
// later ones return at once, so an unreachable collector costs one
// FlushTimeout per request instead of one per flush.
func WithFlushRecord(ctx context.Context) context.Context {
	return context.WithValue(ctx, flushRecordKey{}, new(atomic.Bool))
}

// ForceFlush exports buffered spans and metrics, even for a canceled request.
func (p *Providers) ForceFlush(ctx context.Context) error {
	if p == nil || (p.tracer == nil && p.meter == nil) {
		return nil
	}
	failed, _ := ctx.Value(flushRecordKey{}).(*atomic.Bool)
	if failed != nil && failed.Load() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), FlushTimeout)
	defer cancel()
	var err error
	if p.tracer != nil {
		err = p.tracer.ForceFlush(ctx)
	}
	if p.meter != nil {
		err = errors.Join(err, p.meter.ForceFlush(ctx))
	}
	if err != nil && failed != nil {
		failed.Store(true)
	}
	return err
}

// Shutdown stops every provider Init installed.
func (p *Providers) Shutdown(ctx context.Context) error {
	if p == nil {
		return nil
	}
	var err error
	if p.tracer != nil {
		err = errors.Join(err, p.tracer.Shutdown(ctx))
	}
	if p.meter != nil {
		err = errors.Join(err, p.meter.Shutdown(ctx))
	}
	if p.logger != nil {
		err = errors.Join(err, p.logger.Shutdown(ctx))
	}
	return err
}
