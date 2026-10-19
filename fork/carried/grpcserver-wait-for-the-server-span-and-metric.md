---
id: grpcserver-wait-for-the-server-span-and-metric
fork: kagent
theme: test-flakes
category: test
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: file in the test batch
depends_on: []
commit: ed7ddf2a7
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `test(grpcserver): wait for the server span and metric in telemetry test` | `TestServerTelemetryShape` (upstream's, from kagent-dev/kagent#2929) failed intermittently in `go-unit-tests` with `rpc.server.call.duration was not recorded` (giantswarm/kagent-upstream#220): the client has its response before otelgrpc ends the server span and records the metric on the stats handler's `End` event, and the test read the exporter and the reader right after the calls returned. It waits, bounded, for the GetVersion server span and the metric; the assertions are unchanged. 200 of 200 under `-race`, also under parallel CPU load | to file — prepared in the fork: branch `upstream/grpcserver-telemetry-wait` (mirror `main` + the patch, DCO-signed, no fork references) |
