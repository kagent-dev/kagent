package telemetry

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"

	"github.com/kagent-dev/kagent/go/pkg/logging"
)

func TestSDKDiagnosticsAreBoundedAndKeepCollectorPayloadsPrivate(t *testing.T) {
	restoreGlobals(t)
	setExporters(t, "none", "none", "none")
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	previous := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(previous) })
	providers, err := Init(logging.IntoContext(t.Context(), logger), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := providers.Shutdown(t.Context()); err != nil {
			t.Error(err)
		}
	})
	otel.Handle(nil)
	if output.Len() != 0 {
		t.Fatalf("nil error produced a diagnostic: %s", output.String())
	}
	failure := errors.New(strings.Repeat("collector-secret", 1000))
	var group sync.WaitGroup
	for range 64 {
		group.Go(func() { otel.Handle(failure) })
	}
	group.Wait()
	if got := strings.Count(output.String(), "\n"); got != 1 {
		t.Fatalf("diagnostic count = %d, want 1", got)
	}
	if output.Len() > 512 || strings.Contains(output.String(), "collector-secret") {
		t.Fatalf("unbounded or sensitive diagnostic: %d bytes", output.Len())
	}
	if !strings.Contains(output.String(), "error_type") {
		t.Fatal("missing diagnostic error type")
	}
}

func TestFailedInitializationLeavesProvidersUsable(t *testing.T) {
	restoreGlobals(t)
	setExporters(t, "invalid-exporter", "none", "none")
	providers, err := Init(t.Context(), Options{})
	if err == nil {
		t.Fatal("expected an exporter initialization error")
	}
	if providers == nil || providers.TracesEnabled() {
		t.Fatal("failed exporter installed a provider")
	}
	if err := providers.ForceFlush(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := providers.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestShutdownHonorsDeadlineWithHangingCollector(t *testing.T) {
	restoreGlobals(t)
	setExporters(t, "otlp", "none", "none")
	received, release := make(chan struct{}, 1), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body.Close()
		received <- struct{}{}
		<-release
	}))
	t.Cleanup(server.Close)
	defer close(release)
	providers, err := Init(t.Context(), Options{Config: Config{Traces: &OTLPConfig{
		Endpoint: server.URL, Protocol: "http/protobuf", TimeoutMillis: 60000,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	_, span := otel.Tracer("core").Start(t.Context(), "request")
	span.End()
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	started := time.Now()
	go func() { result <- providers.Shutdown(ctx) }()
	select {
	case <-received:
	case <-ctx.Done():
		t.Fatal("Collector did not receive the shutdown export")
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Shutdown error = %v, want deadline exceeded", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown exceeded the process deadline")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Shutdown took %s, want at most 1s", elapsed)
	}
}
