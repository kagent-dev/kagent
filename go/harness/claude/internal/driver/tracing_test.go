package driver

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/harness/runtime"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTracingGateEnvironment(t *testing.T) {
	gate := &tracingGate{port: 4242}
	for _, test := range []struct {
		name  string
		input []string
		want  string
	}{
		{name: "unset", input: []string{"PATH=/bin"}, want: "prometheus"},
		{name: "none", input: []string{"OTEL_METRICS_EXPORTER=none"}, want: "prometheus"},
		{name: "otlp", input: []string{"OTEL_METRICS_EXPORTER=otlp"}, want: "otlp,prometheus"},
		{name: "already prometheus", input: []string{"OTEL_METRICS_EXPORTER=otlp, prometheus"}, want: "otlp,prometheus"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := append([]string{"OTEL_EXPORTER_PROMETHEUS_PORT=9464"}, test.input...)
			got := gate.environment(input)
			for _, want := range []string{
				"OTEL_METRICS_EXPORTER=" + test.want,
				"OTEL_EXPORTER_PROMETHEUS_HOST=127.0.0.1",
				"OTEL_EXPORTER_PROMETHEUS_PORT=4242",
			} {
				if !slices.Contains(got, want) {
					t.Errorf("environment %q lacks %q", got, want)
				}
			}
			if slices.Contains(got, "OTEL_EXPORTER_PROMETHEUS_PORT=9464") {
				t.Errorf("environment %q keeps the replaced port", got)
			}
		})
	}
}

func TestTracingGateWait(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "# HELP claude_code_session_count_total Count of CLI sessions started")
		if requests.Add(1) > 3 {
			fmt.Fprintln(w, `claude_code_session_count_total{start_type="fresh"} 1`)
		}
	}))
	defer server.Close()

	gate := &tracingGate{url: server.URL, timeout: 5 * time.Second}
	if err := gate.wait(t.Context(), nil); err != nil {
		t.Fatalf("wait() = %v once the session counter is served", err)
	}
	if requests.Load() < 4 {
		t.Fatalf("wait() returned after %d requests, before the counter was served", requests.Load())
	}

	gate = &tracingGate{url: server.URL + "/unready", timeout: 50 * time.Millisecond}
	requests.Store(-1000)
	if err := gate.wait(t.Context(), nil); !errors.Is(err, errNotReady) {
		t.Fatalf("wait() = %v without the session counter, want %v", err, errNotReady)
	}

	done := make(chan struct{})
	close(done)
	gate = &tracingGate{url: "http://127.0.0.1:1/metrics", timeout: time.Minute}
	started := time.Now()
	if err := gate.wait(t.Context(), done); !errors.Is(err, errProcessExited) {
		t.Fatalf("wait() = %v after the process ended, want %v", err, errProcessExited)
	}
	if time.Since(started) > time.Second {
		t.Fatal("wait() did not stop when the process ended")
	}
}

func TestTracingGateReadyReportsUnreadableScrape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, strings.Repeat("x", maxMetricsBytes))
	}))
	defer server.Close()
	gate := &tracingGate{url: server.URL}
	err := gate.ready(t.Context(), &http.Client{Timeout: 5 * time.Second})
	if err == nil || errors.Is(err, errNotReady) {
		t.Fatalf("ready() = %v for an unreadable scrape, want a read error", err)
	}
}

func TestWarnTelemetryNotReadyMarksTheInvocation(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	ctx, span := provider.Tracer("test").Start(t.Context(), "invoke_agent")
	warnTelemetryNotReady(ctx, "timeout", errNotReady)
	span.End()
	events := recorder.Ended()[0].Events()
	if len(events) != 1 || events[0].Name != telemetryNotReadyEvent ||
		!slices.Contains(events[0].Attributes, attribute.String("error.type", "timeout")) {
		t.Fatalf("events = %v", events)
	}
}

// TestProcessDriverAwaitsTracing runs this test binary as a Claude Code that
// registers its tracer provider 200 ms after starting, and records whether the
// prompt arrived before that and whether its input ended before the result.
func TestProcessDriverAwaitsTracing(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "capture")
	executable := filepath.Join(dir, "claude")
	script := fmt.Sprintf("#!/bin/sh\nexec %q -test.run='^TestHelperClaudeProcess$' -- \"$@\"\n", os.Args[0])
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	d := NewProcessDriver(ProcessConfig{
		Executable: executable, Workspace: dir, AwaitTelemetry: true,
		Environment:   []string{"KAGENT_CLAUDE_HELPER=1", "CAPTURE=" + capture, "OTEL_METRICS_EXPORTER=none"},
		MaxEventBytes: 4096, MaxStderrBytes: 4096, InterruptGrace: time.Second,
	})
	outcome, err := d.Run(t.Context(), runtime.Turn{Prompt: "hello"}, &recordingSink{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if outcome.Failure != nil {
		t.Fatalf("Run() outcome = %#v", outcome)
	}
	result, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(result); got != "prompt after tracing ready\ninput ended\nmetrics=prometheus\n" {
		t.Fatalf("helper observed %q", got)
	}
}

func TestHelperClaudeProcess(t *testing.T) {
	if os.Getenv("KAGENT_CLAUDE_HELPER") != "1" {
		t.Skip("helper process for TestProcessDriverAwaitsTracing")
	}
	listener, err := net.Listen("tcp", os.Getenv("OTEL_EXPORTER_PROMETHEUS_HOST")+":"+os.Getenv("OTEL_EXPORTER_PROMETHEUS_PORT"))
	if err != nil {
		os.Exit(2)
	}
	var ready atomic.Bool
	go func() {
		_ = http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if ready.Load() {
				fmt.Fprintln(w, "claude_code_session_count_total 1")
			}
		}))
	}()
	arrived := make(chan time.Time, 1)
	ended := make(chan struct{})
	go func() {
		input := bufio.NewReader(os.Stdin)
		var at time.Time
		if _, err := input.ReadString('\n'); err == nil {
			at = time.Now()
		}
		arrived <- at
		_, _ = io.Copy(io.Discard, input)
		close(ended)
	}()
	time.Sleep(200 * time.Millisecond)
	readyAt := time.Now()
	ready.Store(true)
	order := "prompt after tracing ready"
	if (<-arrived).Before(readyAt) {
		order = "prompt before tracing ready"
	}
	input := "input ended"
	select {
	case <-ended:
	case <-time.After(time.Second):
		input = "input open at the result"
	}
	report := fmt.Sprintf("%s\n%s\nmetrics=%s\n", order, input, os.Getenv("OTEL_METRICS_EXPORTER"))
	if err := os.WriteFile(os.Getenv("CAPTURE"), []byte(report), 0o600); err != nil {
		os.Exit(2)
	}
	fmt.Println(`{"type":"system","subtype":"init","session_id":"11111111-1111-4111-8111-111111111111"}`)
	fmt.Println(`{"type":"result","subtype":"success","session_id":"11111111-1111-4111-8111-111111111111"}`)
	os.Exit(0)
}
