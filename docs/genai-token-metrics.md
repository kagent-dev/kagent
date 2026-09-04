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

Labels (semconv attributes, dots → underscores), aligned with what the upstream Google ADK Python
runtime emits for the same instrument so a single dashboard works across both runtimes:

| Label | Values |
| --- | --- |
| `gen_ai_token_type` | `input`, `output` (output = candidate + reasoning tokens) |
| `gen_ai_operation_name` | `chat` |
| `gen_ai_provider_name` | well-known value, e.g. `openai`, `anthropic`, `gcp.vertex_ai`, `aws.bedrock`, `azure.ai.openai` |
| `gen_ai_request_model` | configured model, e.g. `gpt-4o` |
| `gen_ai_response_model` | model the provider served (falls back to request model) |
| `gen_ai_agent_name` | agent that produced the tokens (the kagent app name) |
| `error_type` | set on failed requests; empty otherwise |

One observation is recorded per LLM call (streaming partial chunks are not double-counted).

## Configuration

- **Runtime**: available on Declarative agents with `runtime: go`. (The Python runtime
  records the same metric from upstream Google ADK.)
- **Gate**: recording and the `/metrics` endpoint are **default-OFF**, matching kagent's other
  observability gates. Set `OTEL_METRICS_ENABLED=true` to turn them on:

  ```yaml
  env:
    - name: OTEL_METRICS_ENABLED
      value: "true"
  ```

- To have Prometheus scrape the endpoint, annotate the Go-runtime agent pods:

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
# exec into a Go-runtime agent pod and curl its metrics endpoint
kubectl exec <go-agent-pod> -- wget -qO- localhost:<agent-port>/metrics | grep gen_ai_client_token_usage
```

Typical output after two chat requests (note `_count` equals the number of LLM calls, not stream
chunks, and the semconv labels including `gen_ai.agent.name`, response model, and an empty
`error.type`):

```text
# HELP gen_ai_client_token_usage Measures the number of input and output tokens used by GenAI requests.
# TYPE gen_ai_client_token_usage histogram
gen_ai_client_token_usage_sum{error_type="",gen_ai_agent_name="my_agent",gen_ai_operation_name="chat",gen_ai_provider_name="gcp.vertex_ai",gen_ai_request_model="gemini-2.5-flash",gen_ai_response_model="gemini-2.5-flash",gen_ai_token_type="input"} 137
gen_ai_client_token_usage_count{error_type="",gen_ai_agent_name="my_agent",gen_ai_operation_name="chat",gen_ai_provider_name="gcp.vertex_ai",gen_ai_request_model="gemini-2.5-flash",gen_ai_response_model="gemini-2.5-flash",gen_ai_token_type="input"} 2
gen_ai_client_token_usage_sum{error_type="",gen_ai_agent_name="my_agent",gen_ai_operation_name="chat",gen_ai_provider_name="gcp.vertex_ai",gen_ai_request_model="gemini-2.5-flash",gen_ai_response_model="gemini-2.5-flash",gen_ai_token_type="output"} 91
gen_ai_client_token_usage_count{error_type="",gen_ai_agent_name="my_agent",gen_ai_operation_name="chat",gen_ai_provider_name="gcp.vertex_ai",gen_ai_request_model="gemini-2.5-flash",gen_ai_response_model="gemini-2.5-flash",gen_ai_token_type="output"} 2
```

## Follow-ups

- Optional OTLP **push** (in addition to the scrape endpoint) via the OpenTelemetry
  [Prometheus→OTLP bridge](https://pkg.go.dev/go.opentelemetry.io/contrib/bridges/prometheus), for
  environments that push to an OTLP collector rather than scrape.