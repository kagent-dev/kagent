# Exporting controller logs over OTLP

The kagent controller can export its own logs to an OpenTelemetry (OTLP) backend, in addition to
stdout. This is useful for shipping controller logs to the same collector/backend as its traces.
It is **off by default**.

> **Note — avoid double ingestion.** The OTLP export is *additive*: logs still go to stdout
> unchanged. If you already collect the controller's stdout (e.g. a Fluent Bit / Vector / Loki
> agent scraping pod logs), enabling this ships the same records a second time over OTLP and your
> backend will ingest them twice. Enable it only if OTLP is your primary log path, or drop the
> controller's stdout logs at your node agent.

## Enabling

Set the following environment variables on the **controller**:

| Variable | Purpose |
| --- | --- |
| `KAGENT_CONTROLLER_OTLP_LOGS_ENABLED=true` | turn on the controller's OTLP log pipeline |
| `OTEL_EXPORTER_OTLP_ENDPOINT` / `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` | collector endpoint |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `grpc` (default) or `http/protobuf` |

> This switch is **decoupled from agent logging**. `OTEL_LOGGING_ENABLED` enables gen_ai
> input/output logging on *agents* and is forwarded to agent pods; it does **not** control the
> controller's own log export. The controller uses its own non-`OTEL_`-prefixed flag
> (`KAGENT_CONTROLLER_OTLP_LOGS_ENABLED`) precisely so the two are configured independently. Via
> Helm these are `otel.logging.enabled` (agents) and `otel.logging.controller.enabled` (controller).

Logs still go to stdout unchanged — the OTLP export is additive (a tee on the controller's zap core
via the [otelzap bridge](https://pkg.go.dev/go.opentelemetry.io/contrib/bridges/otelzap)).

### Severity

The OTLP pipeline ships the **same levels as stdout**: a min-severity processor
([`minsev`](https://pkg.go.dev/go.opentelemetry.io/contrib/processors/minsev)) is set to the
controller's configured `--zap-log-level`, so enabling export at `error` level does not start
shipping `info`/`debug` records.

### Trace correlation (note)

Records carry `trace_id`/`span_id` only when the log call passes the request `context.Context` as a
field. controller-runtime's `logr` → `zapr` path does not thread the reconcile context into fields,
so logs emitted via `log.FromContext(ctx)` are **not** automatically span-linked. Full automatic
correlation would require a separate mechanism and is out of scope for this feature.

A complementary approach — emitting structured JSON to stdout with `trace_id`/`span_id` as fields,
so stdout-based collectors get correlated logs without the OTLP pipeline — is tracked as a
follow-up in [#2349](https://github.com/kagent-dev/kagent/issues/2349).

## Testing it with an OTel Collector

A collector with the `debug` exporter is the quickest way to confirm logs arrive:

```yaml
# otel-collector-config.yaml
receivers:
  otlp:
    protocols:
      grpc: { endpoint: 0.0.0.0:4317 }
      http: { endpoint: 0.0.0.0:4318 }
exporters:
  debug: { verbosity: detailed }
service:
  pipelines:
    logs:
      receivers: [otlp]
      exporters: [debug]
```

```bash
# run the collector, then point the controller at it and enable controller log export
export KAGENT_CONTROLLER_OTLP_LOGS_ENABLED=true
export OTEL_EXPORTER_OTLP_ENDPOINT=http://<collector>:4318
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
# controller logs now appear in the collector's debug output as LogRecords.
```

The controller's log records show up in the `debug` exporter with the shared resource attributes
(`service.name=kagent-controller`, …) and the bridge scope:

```text
Resource attributes:
     -> k8s.namespace.name: Str(kagent)
     -> k8s.pod.name: Str(kagent-controller-...)
     -> service.name: Str(kagent-controller)
     -> service.version: Str(v0.0.0-demo)
InstrumentationScope github.com/kagent-dev/kagent/go/core
LogRecord  SeverityText: info   Body: Str(reconciling Agent)             { controller=agent, agent=k8s-agent, namespace=kagent }
LogRecord  SeverityText: info   Body: Str(Agent reconciled successfully) { controller=agent, agent=k8s-agent }
LogRecord  SeverityText: error  Body: Str(example error log)             { reason=demo }
```
