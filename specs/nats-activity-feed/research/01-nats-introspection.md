# NATS Introspection & Monitoring Capabilities

## System Events (`$SYS` subjects)

NATS server publishes system-level events on `$SYS.>` subjects:

| Subject Pattern | Description |
|----------------|-------------|
| `$SYS.SERVER.*.CONNECT` | Client connected |
| `$SYS.SERVER.*.DISCONNECT` | Client disconnected |
| `$SYS.ACCOUNT.*.CONNECT` | Account-level connect |
| `$SYS.ACCOUNT.*.DISCONNECT` | Account-level disconnect |
| `$SYS.SERVER.*.STATSZ` | Periodic server stats |

Requires system account credentials to subscribe.

## Monitoring HTTP Endpoints

Built-in HTTP monitoring (default port 8222):

| Endpoint | Description |
|----------|-------------|
| `/varz` | Server stats (connections, messages, bytes, CPU, memory) |
| `/connz` | Active connections with per-client metrics |
| `/subsz` | Subscription routing info and subject interest |
| `/routez` | Cluster route information |
| `/healthz` | Health check |
| `/jsz` | JetStream stats (streams, consumers, storage) |

## JetStream Advisory Events

When JetStream is enabled, advisory events are published on `$JS.EVENT.ADVISORY.>`:

| Subject | Description |
|---------|-------------|
| `$JS.EVENT.ADVISORY.STREAM.CREATED.*` | Stream created |
| `$JS.EVENT.ADVISORY.STREAM.DELETED.*` | Stream deleted |
| `$JS.EVENT.ADVISORY.CONSUMER.CREATED.*.*` | Consumer created |
| `$JS.EVENT.ADVISORY.CONSUMER.DELETED.*.*` | Consumer deleted |
| `$JS.EVENT.ADVISORY.API` | API audit trail |

## Subject Discovery & Message Tapping

- **Wildcard subscriptions**: Subscribe to `>` (all) or `agent.>` (prefix) to observe traffic
- **No message modification**: Subscribers are passive observers, don't affect delivery
- **Subject enumeration**: No built-in "list all subjects" — must observe traffic or use `$SYS` events
- **NATS CLI**: `nats sub ">"` taps all messages; `nats events` shows system events

## Key Insight for Activity Feed

For kagent's use case, we don't need $SYS events. Agents already publish structured `StreamEvent` messages on `agent.{name}.{session}.stream`. A simple wildcard subscription to `agent.>` captures all agent activity without any server-level introspection.

## Sources

- NATS Docs: Monitoring (docs.nats.io/running-a-nats-service/configuration/monitoring)
- NATS Docs: System Events (docs.nats.io/running-a-nats-service/configuration/sys_accounts)
- NATS Docs: JetStream (docs.nats.io/nats-concepts/jetstream)
