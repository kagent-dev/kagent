# Implementation Plan: NATS Activity Feed

## Checklist

- [x] Step 1: Project scaffold + config + health endpoint
- [x] Step 2: SSE hub with ring buffer
- [x] Step 3: NATS subscriber → FeedEvent → hub broadcast
- [x] Step 4: Embedded SPA UI
- [x] Step 5: Dockerfile + Helm chart
- [x] Step 6: Integration test with embedded NATS

---

## Step 1: Project scaffold + config + health endpoint

**Objective:** Bootable binary with CLI flags, env vars, and `/healthz`.

**Implementation:**
- Create `go/plugins/nats-activity-feed/` directory structure (see design appendix B)
- `internal/config/config.go` — parse `--nats-addr`, `--addr`, `--buffer-size`, `--subject` with env fallbacks
- `main.go` — wire config → HTTP mux → listen
- Register `GET /healthz` returning 200

**Tests:** Config parsing unit test (flags and env vars).

**Demo:** `go run ./go/plugins/nats-activity-feed/ --help` shows flags; `curl :8090/healthz` returns 200.

---

## Step 2: SSE hub with ring buffer

**Objective:** Working SSE endpoint that broadcasts FeedEvents to connected browsers.

**Implementation:**
- `internal/sse/hub.go` — adapt kanban-mcp hub pattern:
  - `Subscribe()` returns channel, sends ring buffer contents as initial burst
  - `Broadcast(FeedEvent)` appends to ring buffer, fans out to subscribers
  - `ServeSSE(w, r)` handles `/events` endpoint
- Define `FeedEvent` struct in `internal/feed/types.go`
- Register `GET /events` in main.go

**Tests:** Hub unit test — subscribe, broadcast 3 events, verify receipt; ring buffer overflow test.

**Demo:** `curl -N :8090/events` stays open; posting a test event via hub shows up in curl output.

---

## Step 3: NATS subscriber → FeedEvent → hub broadcast

**Objective:** Connect to NATS, subscribe to `agent.>`, parse events, broadcast to hub.

**Implementation:**
- `internal/feed/subscriber.go`:
  - `NewSubscriber(natsAddr, subject, hub)` — connects to NATS, subscribes
  - NATS message handler: parse subject (`agent.{name}.{session}.stream`), unmarshal `StreamEvent`, construct `FeedEvent`, call `hub.Broadcast()`
  - Handle parse errors gracefully (log + skip)
- `main.go` — create NATS connection, start subscriber before HTTP listen
- Use `nats.MaxReconnects(-1)` for auto-reconnect

**Tests:** Subject parser unit test (valid subjects, malformed subjects). Integration test deferred to Step 6.

**Demo:** Run binary with NATS, use `nats pub agent.test.sess1.stream '{"type":"token","data":"hello","timestamp":1234}'`, see event in `curl -N :8090/events`.

---

## Step 4: Embedded SPA UI

**Objective:** Browser UI showing live activity feed.

**Implementation:**
- `internal/ui/index.html` — single-file SPA:
  - Connect to `/events` SSE with auto-reconnect
  - Render events as scrolling list (newest at top, cap at 500 visible)
  - Each row: timestamp (HH:MM:SS.mmm), agent name badge, event type badge (color-coded), data preview (truncated)
  - Color scheme: token=gray, tool_start=blue, tool_end=green, error=red, approval_request=orange, completion=purple
  - Empty state: "Waiting for activity..."
  - Optional controls: pause/resume toggle, event type filter checkboxes, clear button
- `internal/ui/embed.go` — `//go:embed index.html`
- Register `GET /` to serve embedded HTML

**Tests:** Embed test (file exists and is non-empty, same as kanban-mcp).

**Demo:** Open `http://localhost:8090` in browser, trigger agent activity, see live feed.

---

## Step 5: Dockerfile + Helm chart

**Objective:** Deployable to Kubernetes alongside kagent.

**Implementation:**
- `go/plugins/nats-activity-feed/Dockerfile` — multi-stage Go build (copy from kanban-mcp pattern)
- `helm/tools/nats-activity-feed/` — Helm chart:
  - Deployment with configurable NATS address (default `nats://nats:4222`)
  - Service on port 8090
  - Values: `natsAddr`, `bufferSize`, `subject`, `resources`
  - Only deployed when `temporal.enabled=true` (NATS dependency)

**Tests:** `helm template` lint passes.

**Demo:** `make helm-install` deploys feed alongside kagent; `kubectl port-forward svc/nats-activity-feed 8090:8090` opens the feed.

---

## Step 6: Integration test with embedded NATS

**Objective:** End-to-end test proving NATS → subscriber → hub → SSE works.

**Implementation:**
- `internal/feed/subscriber_test.go` — integration test:
  - Start embedded NATS server (same pattern as `go/adk/pkg/streaming/nats_test.go`)
  - Create hub + subscriber
  - Subscribe to hub SSE channel
  - Publish test `StreamEvent` to NATS on `agent.test-agent.session-1.stream`
  - Assert `FeedEvent` received with correct agent name, session, event type, data
  - Test malformed messages are skipped without error

**Tests:** This IS the test step.

**Demo:** `go test ./go/plugins/nats-activity-feed/... -v` passes.
