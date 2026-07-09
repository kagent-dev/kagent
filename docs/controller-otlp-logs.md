# Exporting controller logs over OTLP

The kagent controller can export its own logs to an OpenTelemetry (OTLP) backend, in addition to
stdout. This is useful for shipping controller logs to the same collector/backend as its traces.
It is **off by default**.

## Enabling

Set the standard OTel environment variables on the controller:

| Variable | Purpose |
| --- | --- |
| `OTEL_LOGGING_ENABLED=true` | turn on the OTLP log pipeline |
| `OTEL_EXPORTER_OTLP_ENDPOINT` / `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` | collector endpoint |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `grpc` (default) or `http/protobuf` |

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
# run the collector, then point the controller at it and enable logging
export OTEL_LOGGING_ENABLED=true
export OTEL_EXPORTER_OTLP_ENDPOINT=http://<collector>:4318
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
# controller logs now appear in the collector's debug output as LogRecords,
# with the same resource attributes (service.name=kagent-controller, service.version, ...)
# as the controller's traces.
```

<!-- TODO: add a screenshot of the collector debug exporter showing controller LogRecords once a
     cluster/collector is available to capture one. -->
