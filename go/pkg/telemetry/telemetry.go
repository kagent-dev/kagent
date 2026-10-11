// Package telemetry boots the OpenTelemetry SDK from explicit configuration with standard OTEL environment overrides.
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
)

// FlushTimeout bounds one ForceFlush.
const FlushTimeout = 3 * time.Second

// Options identify the process. The environment wins over Defaults, but Identity
// contains compiler-owned attributes that file and environment settings cannot override.
// MetricReaders are added beside the reader the environment configures.
type Options struct {
	Config         Config
	Identity       []attribute.KeyValue
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

// Init applies configuration defaults, then installs the global propagator and one provider
// per enabled signal.
// A signal that fails to configure stays off and is reported in the error.
func Init(ctx context.Context, opts Options) (*Providers, error) {
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		slog.Default().ErrorContext(context.Background(), "opentelemetry", "error", err)
	}))
	providers := &Providers{}
	if err := opts.Config.Validate(); err != nil {
		return providers, err
	}
	// The SDK owns parsing OTEL_* (including headers, trust, sampling, and
	// batching). Translate file settings into defaults once at startup.
	for _, entry := range Environment(os.Environ(), opts.Config) {
		key, value, ok := strings.Cut(entry, "=")
		if ok && strings.HasPrefix(key, "OTEL_") {
			if err := os.Setenv(key, value); err != nil {
				return providers, fmt.Errorf("configure telemetry %s: %w", key, err)
			}
		}
	}
	otel.SetTextMapPropagator(autoprop.NewTextMapPropagator())
	if strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_SDK_DISABLED")), "true") { //nolint:forbidigo // Standard SDK environment override at startup.
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
	}
	for key, value := range opts.Config.ResourceAttributes {
		detectors = append(detectors, resource.WithAttributes(attribute.String(key, value)))
	}
	detectors = append(detectors, resource.WithFromEnv(), resource.WithAttributes(opts.Identity...))
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

// ForceFlush exports buffered logs, spans and metrics, even for a canceled
// request. Logs go first because audit events are the telemetry most likely
// to be lost when the process is suspended right after the response closes.
func (p *Providers) ForceFlush(ctx context.Context) error {
	if p == nil || (p.tracer == nil && p.meter == nil && p.logger == nil) {
		return nil
	}
	failed, _ := ctx.Value(flushRecordKey{}).(*atomic.Bool)
	if failed != nil && failed.Load() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), FlushTimeout)
	defer cancel()
	var err error
	if p.logger != nil {
		err = p.logger.ForceFlush(ctx)
	}
	if p.tracer != nil {
		err = errors.Join(err, p.tracer.ForceFlush(ctx))
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
