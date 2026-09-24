package driver

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	// sessionCountMetric is the Prometheus name of claude_code.session.count.
	sessionCountMetric = "claude_code_session_count"
	// tracingReadyTimeout caps the delay a stalled managed settings fetch adds
	// to a turn, which then runs without its Claude spans.
	tracingReadyTimeout = 10 * time.Second
	tracingReadyPoll    = 10 * time.Millisecond
	maxMetricsBytes     = 4 << 20
)

// tracingGate holds a turn's prompt until Claude Code can trace it.
//
// Claude Code starts a print-mode turn without waiting for its OpenTelemetry
// initialization, which with first-party credentials first waits on a managed
// settings fetch. Spans started earlier are never recorded, and TRACEPARENT is
// ignored. Claude increments claude_code.session.count only after the tracer
// provider and propagator are registered, so the counter appearing on a
// loopback Prometheus endpoint means the turn will be traced.
type tracingGate struct {
	port    int
	url     string
	timeout time.Duration
}

// newTracingGate reserves a loopback port for Claude Code's Prometheus reader.
func newTracingGate() (*tracingGate, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("reserve Claude metrics port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return nil, fmt.Errorf("release Claude metrics port: %w", err)
	}
	return &tracingGate{
		port: port, url: fmt.Sprintf("http://127.0.0.1:%d/metrics", port), timeout: tracingReadyTimeout,
	}, nil
}

// environment adds the gate's Prometheus reader to any configured metrics
// exporter.
func (g *tracingGate) environment(environment []string) []string {
	exporters := []string{"prometheus"}
	for _, item := range environment {
		value, ok := strings.CutPrefix(item, "OTEL_METRICS_EXPORTER=")
		if !ok {
			continue
		}
		exporters = exporters[:0]
		for exporter := range strings.SplitSeq(value, ",") {
			if exporter = strings.TrimSpace(exporter); exporter != "" && exporter != "none" {
				exporters = append(exporters, exporter)
			}
		}
		if !slices.Contains(exporters, "prometheus") {
			exporters = append(exporters, "prometheus")
		}
	}
	environment = replaceEnvironment(environment, "OTEL_METRICS_EXPORTER", strings.Join(exporters, ","))
	environment = replaceEnvironment(environment, "OTEL_EXPORTER_PROMETHEUS_HOST", "127.0.0.1")
	return replaceEnvironment(environment, "OTEL_EXPORTER_PROMETHEUS_PORT", strconv.Itoa(g.port))
}

var (
	errNotReady      = errors.New("claude_code.session.count is not served yet")
	errProcessExited = errors.New("claude exited before its tracing was ready")
)

// wait returns nil once Claude Code has registered its tracer provider. It
// returns the last readiness error when the timeout passes, the context error
// when the context ends, and errProcessExited when done closes.
func (g *tracingGate) wait(ctx context.Context, done <-chan struct{}) error {
	client := &http.Client{Timeout: time.Second}
	deadline := time.NewTimer(g.timeout)
	defer deadline.Stop()
	poll := time.NewTicker(tracingReadyPoll)
	defer poll.Stop()
	for {
		err := g.ready(ctx, client)
		if err == nil {
			return nil
		}
		select {
		case <-poll.C:
		case <-deadline.C:
			return fmt.Errorf("not ready after %s: %w", g.timeout, err)
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			return errProcessExited
		}
	}
}

// ready scrapes Claude Code's metrics once and returns nil when the session
// counter is served.
func (g *tracingGate) ready(ctx context.Context, client *http.Client) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, g.url, nil)
	if err != nil {
		return fmt.Errorf("build Claude metrics request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("scrape Claude metrics: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("scrape Claude metrics: %s", response.Status)
	}
	scanner := bufio.NewScanner(io.LimitReader(response.Body, maxMetricsBytes))
	scanner.Buffer(make([]byte, 0, 64<<10), maxMetricsBytes)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), sessionCountMetric) {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read Claude metrics: %w", err)
	}
	return errNotReady
}

// replaceEnvironment returns environment with name set to value.
func replaceEnvironment(environment []string, name, value string) []string {
	prefix := name + "="
	result := make([]string, 0, len(environment)+1)
	for _, item := range environment {
		if !strings.HasPrefix(item, prefix) {
			result = append(result, item)
		}
	}
	return append(result, prefix+value)
}
