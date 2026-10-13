package telemetry

import (
	"context"
	"sync"
	"testing"
	"time"

	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

type recordingLogExporter struct {
	mu    sync.Mutex
	count int
}

func (e *recordingLogExporter) Export(_ context.Context, records []sdklog.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.count += len(records)
	return nil
}

func (e *recordingLogExporter) Shutdown(context.Context) error   { return nil }
func (e *recordingLogExporter) ForceFlush(context.Context) error { return nil }

func (e *recordingLogExporter) exported() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.count
}

func TestForceFlushExportsBufferedLogs(t *testing.T) {
	exporter := &recordingLogExporter{}
	logger := sdklog.NewLoggerProvider(sdklog.WithProcessor(
		sdklog.NewBatchProcessor(exporter, sdklog.WithExportInterval(time.Hour)),
	))
	t.Cleanup(func() { _ = logger.Shutdown(context.Background()) })
	providers := &Providers{logger: logger}

	var record otellog.Record
	record.SetSeverity(otellog.SeverityInfo)
	logger.Logger("test").Emit(t.Context(), record)

	// The request context is already canceled, as it is when deferred cleanup runs.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := providers.ForceFlush(ctx); err != nil {
		t.Fatal(err)
	}
	if got := exporter.exported(); got != 1 {
		t.Fatalf("logs exported after ForceFlush = %d, want 1", got)
	}
}
