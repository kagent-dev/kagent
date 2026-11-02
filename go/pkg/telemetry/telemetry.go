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
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/contrib/propagators/autoprop"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
)

// FlushTimeout bounds one ForceFlush.
const FlushTimeout = 3 * time.Second

// Options identify the process. The environment wins over Defaults, but Identity
// contains compiler-owned attributes that file and environment settings cannot override.
// MetricReaders are added beside the reader the environment configures.
// DeferMetrics holds metrics back until StartMetrics.
type Options struct {
	Config         Config
	Identity       []attribute.KeyValue
	Defaults       []attribute.KeyValue
	SpanProcessors []sdktrace.SpanProcessor
	MetricReaders  []sdkmetric.Reader
	DeferMetrics   bool
}

// Providers holds the SDK providers Init installed, nil for a disabled signal.
type Providers struct {
	tracer *sdktrace.TracerProvider
	meter  atomic.Pointer[sdkmetric.MeterProvider]
	logger *sdklog.LoggerProvider

	startMetrics   sync.Once
	metricResource *resource.Resource
	metricReaders  []sdkmetric.Reader
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
	if disabled, _ := env.OTELSDKDisabled.Lookup(); strings.EqualFold(strings.TrimSpace(disabled), "true") {
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
	if opts.DeferMetrics {
		providers.metricResource, providers.metricReaders = res, opts.MetricReaders
		// Report a bad exporter setting at startup; StartMetrics builds the readers again after the restore.
		probe, err := metricReaders(ctx, nil)
		if err != nil {
			errs = append(errs, err)
		}
		for _, reader := range probe {
			_ = reader.Shutdown(ctx)
		}
	} else {
		readers, err := metricReaders(ctx, opts.MetricReaders)
		if err != nil {
			errs = append(errs, err)
		}
		if len(readers) > 0 {
			providers.setMeter(res, readers)
		}
	}
	if exporter, err := autoexport.NewLogExporter(ctx); err != nil {
		errs = append(errs, fmt.Errorf("logs: %w", err))
	} else if !autoexport.IsNoneLogExporter(exporter) {
		providers.logger = sdklog.NewLoggerProvider(sdklog.WithResource(res), sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)))
		otel.SetLoggerProvider(providers.logger)
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

func metricReaders(ctx context.Context, extra []sdkmetric.Reader) ([]sdkmetric.Reader, error) {
	readers := slices.Clone(extra)
	reader, err := autoexport.NewMetricReader(ctx)
	if err != nil {
		return readers, fmt.Errorf("metrics: %w", err)
	}
	if !autoexport.IsNoneMetricReader(reader) {
		readers = append(readers, reader)
	}
	return readers, nil
}

func (p *Providers) setMeter(res *resource.Resource, readers []sdkmetric.Reader) {
	options := []sdkmetric.Option{sdkmetric.WithResource(res)}
	for _, reader := range readers {
		options = append(options, sdkmetric.WithReader(reader))
	}
	meter := sdkmetric.NewMeterProvider(options...)
	p.meter.Store(meter)
	otel.SetMeterProvider(meter)
}

// StartMetrics installs the meter provider that DeferMetrics held back, with a new
// service.instance.id, so each restored process writes its own series. Only the first call acts.
func (p *Providers) StartMetrics() {
	if p == nil {
		return
	}
	p.startMetrics.Do(func() {
		if p.metricResource == nil {
			return
		}
		readers, err := metricReaders(context.Background(), p.metricReaders)
		if err != nil {
			otel.Handle(err)
		}
		if len(readers) == 0 {
			return
		}
		// A schemaless resource cannot conflict, so Merge cannot fail.
		res, _ := resource.Merge(p.metricResource, resource.NewSchemaless(semconv.ServiceInstanceID(uuid.NewString())))
		p.setMeter(res, readers)
	})
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
	if p == nil {
		return nil
	}
	meter := p.meter.Load()
	if p.tracer == nil && meter == nil && p.logger == nil {
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
	if meter != nil {
		err = errors.Join(err, meter.ForceFlush(ctx))
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
	if meter := p.meter.Load(); meter != nil {
		err = errors.Join(err, meter.Shutdown(ctx))
	}
	if p.logger != nil {
		err = errors.Join(err, p.logger.Shutdown(ctx))
	}
	return err
}
