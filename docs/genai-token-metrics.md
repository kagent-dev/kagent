# GenAI token-usage metrics

kagent's **Go ADK** agent runtime records the OpenTelemetry GenAI-semconv metric
[`gen_ai.client.token.usage`](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-metrics/#metric-gen_aiclienttokenusage)
using the native Prometheus client library and exposes it for scraping. It lets you graph and alert
on token spend per model / provider without parsing traces.

## What is emitted

A Prometheus histogram, served at **`/metrics`** on the agent's HTTP port:

| Prometheus name | OTel semconv | Notes |
| --- | --- | --- |
| `gen_ai_client_token_usage` | `gen_ai.client.token.usage` | histogram, semconv-recommended buckets |

Labels (semconv attributes, dots → underscores):

| Label | Values |
| --- | --- |
| `gen_ai_token_type` | `input`, `output` (output = candidate + reasoning tokens) |
| `gen_ai_operation_name` | `chat` |
| `gen_ai_provider_name` | well-known value, e.g. `openai`, `anthropic`, `gcp.vertex_ai`, `aws.bedrock`, `azure.ai.openai` |
| `gen_ai_request_model` | configured model, e.g. `gpt-4o` |
| `gen_ai_response_model` | model the provider served (falls back to request model) |
| `error_type` | set on failed requests; empty otherwise |

One observation is recorded per LLM call (streaming partial chunks are not double-counted).

## Configuration

- **Runtime**: available on Declarative agents with `runtime: go`. (The Python runtime is not yet
  instrumented.)
- **No flag to enable**: the `/metrics` endpoint is always served, and the controller adds Prometheus
  pod annotations to Go-runtime agent Deployments automatically:

  ```yaml
  metadata:
    annotations:
      prometheus.io/scrape: "true"
      prometheus.io/port: "<agent-port>"
      prometheus.io/path: "/metrics"
  ```

### Scraping it

Any Prometheus-compatible scraper that honors pod annotations will pick agents up. With an
OpenTelemetry Collector, add a `prometheus` receiver job with pod discovery:

```yaml
receivers:
  prometheus:
    config:
      scrape_configs:
        - job_name: kagent-agents
          kubernetes_sd_configs: [{ role: pod }]
          relabel_configs:
            - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_scrape]
              regex: "true"
              action: keep
            - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_path]
              target_label: __metrics_path__
            - source_labels: [__address__, __meta_kubernetes_pod_annotation_prometheus_io_port]
              regex: ([^:]+)(?::\d+)?;(\d+)
              replacement: $$1:$$2
              target_label: __address__
```

## Verifying

```bash
# 1. exec into a Go-runtime agent pod and curl its metrics endpoint
kubectl exec <go-agent-pod> -- wget -qO- localhost:<agent-port>/metrics | grep gen_ai_client_token_usage

# 2. after chatting with the agent you should see series like:
# gen_ai_client_token_usage_count{gen_ai_token_type="input",gen_ai_provider_name="gcp.vertex_ai",
#   gen_ai_request_model="gemini-2.5-flash",gen_ai_response_model="gemini-2.5-flash",
#   gen_ai_operation_name="chat",error_type=""} 1
```

<!-- TODO: add a Grafana/Prometheus screenshot of gen_ai_client_token_usage once a cluster with a
     scrape target is available. -->

## Follow-ups

- Optional OTLP **push** (in addition to the scrape endpoint) via the OpenTelemetry
  [Prometheus→OTLP bridge](https://pkg.go.dev/go.opentelemetry.io/contrib/bridges/prometheus), for
  environments that push to an OTLP collector rather than scrape.
- Python ADK runtime instrumentation.
