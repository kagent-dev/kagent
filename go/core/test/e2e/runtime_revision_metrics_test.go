package e2e_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
)

// TestRuntimeRevisionGCMetricsLifecycle requires an isolated installation with
// secure metrics enabled and a direct scrape target for the active GC leader.
func TestRuntimeRevisionGCMetricsLifecycle(t *testing.T) {
	endpoint := os.Getenv("KAGENT_E2E_METRICS_URL")
	if endpoint == "" {
		t.Skip("set KAGENT_E2E_METRICS_URL to exercise the actual authenticated controller metrics listener")
	}
	parsed, err := url.Parse(endpoint)
	require.NoError(t, err)
	require.Equal(t, "https", parsed.Scheme, "this test exercises authenticated TLS metrics, not an insecure substitute")
	require.Equal(t, "/metrics", parsed.Path)
	require.Nil(t, parsed.User, "supply metrics credentials through the token file")
	tokenFile := os.Getenv("KAGENT_E2E_METRICS_TOKEN_FILE")
	require.NotEmpty(t, tokenFile, "provide a token with get permission on /metrics")
	tlsConfig := rest.TLSClientConfig{CAFile: os.Getenv("KAGENT_E2E_METRICS_CA_FILE")}
	transport, err := rest.TransportFor(&rest.Config{TLSClientConfig: tlsConfig, BearerTokenFile: tokenFile})
	require.NoError(t, err)
	client := &http.Client{
		Transport: transport, Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	t.Cleanup(client.CloseIdleConnections)
	anonymousTransport, err := rest.TransportFor(&rest.Config{TLSClientConfig: tlsConfig})
	require.NoError(t, err)
	anonymous := &http.Client{Transport: anonymousTransport, Timeout: 15 * time.Second, CheckRedirect: client.CheckRedirect}
	t.Cleanup(anonymous.CloseIdleConnections)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, endpoint, nil)
	require.NoError(t, err)
	response, err := anonymous.Do(request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, response.StatusCode,
		"the metrics listener must reject anonymous scrapes")

	const (
		pendingMetric  = "kagent_runtime_revision_gc_pending"
		ageMetric      = "kagent_runtime_revision_gc_oldest_pending_age_seconds"
		observedMetric = "kagent_runtime_revision_gc_last_successful_backlog_observation_timestamp_seconds"
		failuresMetric = "kagent_runtime_revision_gc_failures_total"
	)
	scrape := func(ctx context.Context) map[string]float64 {
		t.Helper()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		require.NoError(t, err)
		request.Header.Set("Accept", "text/plain; version=0.0.4")
		response, err := client.Do(request)
		require.NoError(t, err)
		defer response.Body.Close()
		require.Equal(t, http.StatusOK, response.StatusCode)
		parser := expfmt.NewTextParser(model.LegacyValidation)
		families, err := parser.TextToMetricFamilies(io.LimitReader(response.Body, 4<<20))
		require.NoError(t, err)
		gauges := make(map[string]float64)
		for _, name := range []string{pendingMetric, ageMetric, observedMetric} {
			family := families[name]
			require.NotNil(t, family, "metric %s missing; target the active GC leader", name)
			require.Len(t, family.GetMetric(), 1)
			metric := family.GetMetric()[0]
			require.Empty(t, metric.GetLabel())
			require.NotNil(t, metric.Gauge)
			gauges[name] = metric.GetGauge().GetValue()
		}
		require.Positive(t, gauges[observedMetric], "an empty 200 response or an uninitialized registry does not prove metrics")
		failures := families[failuresMetric]
		require.NotNil(t, failures)
		require.Len(t, failures.GetMetric(), 7)
		for _, metric := range failures.GetMetric() {
			require.NotNil(t, metric.Counter)
			require.Len(t, metric.GetLabel(), 1)
			require.Equal(t, "stage", metric.GetLabel()[0].GetName())
		}
		return gauges
	}
	require.NoError(t, wait.PollUntilContextTimeout(t.Context(), time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
		return scrape(ctx)[pendingMetric] == 0, nil
	}), "this aggregate-metrics test requires an isolated installation without unrelated pending cleanup")
	testRuntimeRevisionLifecycle(t, func(ctx context.Context, releasedAt time.Time) {
		require.NoError(t, wait.PollUntilContextTimeout(ctx, time.Second, time.Minute, true, func(ctx context.Context) (bool, error) {
			gauges := scrape(ctx)
			return gauges[pendingMetric] == 0 && gauges[ageMetric] == 0 &&
				gauges[observedMetric] >= float64(releasedAt.UnixNano())/float64(time.Second), nil
		}), "a fresh post-cleanup observation must report zero pending count and age")
	})
}
