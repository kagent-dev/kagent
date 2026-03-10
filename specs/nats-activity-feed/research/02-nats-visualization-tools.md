# Existing NATS Visualization Tools

## nats-top (CLI)
- Real-time terminal UI showing connections, subscriptions, throughput
- Lightweight, no setup
- **Limitation**: Terminal-only, no message inspection, no history

## nats-surveyor (Prometheus Exporter)
- Polls NATS system account, exports to Prometheus/Grafana
- Enterprise-grade with pre-built dashboards
- **Limitation**: Heavyweight stack, no message-level detail, not real-time feed

## nats-dashboard (Browser)
- Static web app inspired by nats-top
- Polls HTTP monitoring endpoint
- PWA-capable, no backend
- **Limitation**: No message inspection, no history, snapshot-only

## nats-ui (Community)
- Modern web GUI with WebSocket NATS connection
- Publish/subscribe from browser, JetStream management
- **Limitation**: Requires WebSocket port, management-focused not activity-focused

## Gap Analysis

**None of these tools provide:**
1. Live chronological message feed (they show aggregate metrics)
2. Message payload preview with structured formatting
3. Filtered, searchable activity history
4. Agent-aware context (who published, which session, what tool)

A kagent activity feed would show **what agents are doing right now** — tool calls, LLM tokens, approvals, errors — in a live stream, not server metrics.
