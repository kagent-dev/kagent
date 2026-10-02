# A2A push notifications

Push notifications let a client leave a long-running task and receive a webhook
when the task needs attention or finishes. The Agent Card advertises support with
`capabilities.pushNotifications: true`. Callbacks are scoped to one task, not to
every task in its conversation.

## Set up a callback

1. Expose a webhook that accepts an HTTP POST and verifies a Bearer credential.
2. Supply a `taskPushNotificationConfig` with an HTTPS URL and that credential
   on `SendMessage` or `SendStreamingMessage`. Kagent saves the registration
   before dispatch and attaches it to a task only when that task accepts the
   message. The config's `taskId` must be empty, even when the message
   continues an existing task.
3. Alternatively, call `CreateTaskPushNotificationConfig` for an **active** task.
   Get, List, and Delete work through either public A2A transport. Create is
   rejected once the task is terminal.

For example, send this JSON-RPC body to `/agents/{namespace}/{name}` using the
deployment's normal A2A authentication:

```json
{
  "jsonrpc": "2.0",
  "id": "request-1",
  "method": "SendMessage",
  "params": {
    "message": {"messageId": "message-1", "role": "ROLE_USER", "parts": [{"text": "Hello"}]},
    "configuration": {
      "returnImmediately": true,
      "taskPushNotificationConfig": {
        "url": "https://receiver.example/callback",
        "authentication": {"scheme": "Bearer", "credentials": "receiver-secret"}
      }
    }
  }
}
```

The CLI offers the same initial-send flow with
`kagent agent invoke --session SESSION_ID --task TEXT --push-url URL --push-bearer-token-file PATH`.
For an existing active task, use
`kagent agent session push create SESSION_ID TASK_ID --url URL --bearer-token-file PATH`;
`push get`, `push list`, and `push delete` manage its callbacks.

## What happens

For a callback embedded in `SendMessage`, Kagent first stores a registration
identified by the input message. It does not attach that registration to a task
just because the message names one: the task write must record that it accepted
the input. That write binds the callback to the task in the same database
transaction. A failed or unaccepted send therefore leaves no callback active on
an existing task. If a registration arrives after its message was accepted, a
maintenance worker can recover the association from the accepted message event.
Explicit task-level Create already identifies the task and does not use this
message-binding step.

Binding does not send a webhook. When a later eligible task state is published,
Kagent queues its status update for callbacks bound to that task.

```mermaid
sequenceDiagram
    participant Client
    participant Kagent
    participant Agent
    participant Webhook
    Client->>Kagent: SendMessage with callback
    Note over Kagent: Save registration for input message
    Kagent->>Agent: Start or continue task
    Agent-->>Kagent: Task write accepts message
    Note over Kagent: Bind callback to accepting task
    Kagent-->>Client: Task ID (client may disconnect)
    Agent-->>Kagent: Eligible task state
    Note over Kagent: Publish state and queue callback together
    Kagent->>Webhook: POST statusUpdate with Bearer credential
    Webhook-->>Kagent: 2xx acknowledgement
    Webhook->>Kagent: Authenticated GetTask(task ID)
    Kagent-->>Webhook: Current task and artifacts
```

Kagent sends a JSON A2A `statusUpdate` for `INPUT_REQUIRED`, `AUTH_REQUIRED`, or
a terminal state (`COMPLETED`, `FAILED`, `CANCELED`, or `REJECTED`). The update
contains the task ID and status; it may contain a status message. Treat it as a
signal to call authenticated `GetTask` for the current result and artifacts.
Ordinary progress updates do not trigger callbacks.

```mermaid
flowchart LR
    A[Active task with callback] --> B[Eligible state published]
    B --> C[Callback queued]
    C --> D[POST attempt]
    D -->|2xx| E[Acknowledged]
    D -->|Failure| F[Retry, up to 10 attempts]
    F --> D
    F -->|Attempts exhausted| G[Delivery stops]
```

## Guarantees and client responsibilities

- An embedded callback receives only eligible states published **after its
  message is accepted and the callback is bound to the task**. Explicit Create
  subscribes when the config is saved. Neither path replays earlier states.
  New tasks in the same conversation need their own callback; forks do not
  inherit callbacks.
- Delivery is best effort. Failed requests retry up to ten times, including
  across controller restarts. A callback can arrive more than once or out of
  order. Use `GetTask` as the source of truth and make webhook handling idempotent.
- Any 2xx response acknowledges delivery. Keep the receiver available, validate
  the callback, and respond promptly. Kagent does not expose delivery status.
- An embedded callback without an ID gets a stable ID for retries of the same
  message. Task-level Create without an ID generates a new ID each time; supply
  your own ID if you may retry Create. Multiple IDs can subscribe to one task.
- Creating a config with an existing ID replaces its URL or credentials. Delete
  removes an active callback. Queued deliveries for an old or deleted config are
  canceled, but a request already in flight may still arrive.

## Security

The webhook must verify `Authorization: Bearer <credentials>`. An optional
`token` is also sent in `A2A-Notification-Token` for additional validation.
Create, Get, and List responses omit both secrets; keep them securely on the
client and supply new values when replacing a config. Task reads still require
normal A2A authorization.

Callback URLs require HTTPS. The controller blocks private, loopback, and
link-local destinations by default. Operators can allow trusted internal
receivers with `KAGENT_A2A_PUSH_ALLOW_PRIVATE_NETWORKS=true` and allow HTTP for
local development with the separate `KAGENT_A2A_PUSH_ALLOW_HTTP=true` setting.

## Appendix: how the components work together

The A2A gateway handles the public send and callback-management methods. The
session service authorizes the request, checks the callback configuration, and
resolves the conversation or task. PostgreSQL keeps registrations, accepted
task events, and pending deliveries. The agent runtime writes task events
through the task store; after runtime cleanup, the task store publishes an
eligible state and queues its callbacks in one database transaction. A
leader-elected delivery worker sends those queued callbacks to the receiver.

```mermaid
sequenceDiagram
    autonumber
    participant C as A2A client
    participant G as A2A gateway
    participant S as Session service
    participant DB as PostgreSQL
    participant R as Agent runtime
    participant T as Task store
    participant W as Delivery worker
    participant H as Webhook receiver

    alt Callback embedded in a send
        C->>G: SendMessage with callback and message ID
        G->>S: Prepare authorized send
        S->>DB: Save callback receipt for message
        Note over DB: Receipt has no task association yet
        G->>R: Dispatch message
        R->>T: Write task event accepting message
        T->>DB: Commit accepted event and bind receipt to task
        opt Receipt arrived after the accepting task write
            W->>DB: Find accepted event and bind receipt
        end
    else Create for an existing active task
        C->>G: CreateTaskPushNotificationConfig
        G->>S: Authorize and validate task callback
        S->>DB: Save callback already bound to task
    end

    R->>T: Write waiting or final task state
    Note over T: Finish runtime cleanup before publication
    T->>DB: Publish state and insert callback deliveries atomically
    W->>DB: Claim due delivery with a lease
    DB-->>W: Saved status update and destination
    W->>H: POST statusUpdate with callback credential
    H-->>W: 2xx or failure
    W->>DB: Record success or schedule retry
    opt Receiver handles callback
        H->>G: Authenticated GetTask
        G-->>H: Current task and artifacts
    end
```

The accepted message event is the link between an embedded callback and its
task. A send can name an existing task without being accepted by it, so saving
the receipt alone does not activate the callback. The task write normally binds
them together. If the receipt arrives after that write, the worker can find the
accepted event and bind it later. An unresolved receipt eventually expires;
binding itself never sends or replays a callback.

The receipt keeps a fingerprint of the original send. Retrying the same message
with the same callback is safe, while retrying it with different callback data
is rejected; a retry also cannot undo a later edit or deletion. For an explicit
Create, the task and callback ID identify the configuration to add or replace.

Each published eligible state creates one durable delivery per active callback.
The delivery contains the status update and a snapshot of the destination, so
HTTP runs outside the task transaction. A failed attempt remains queued for
retry with exponential backoff. The worker leases a delivery before sending;
an expired lease allows recovery after a crash, while a claim token prevents an
older worker from recording an outcome over a newer attempt. Replacing or
deleting a callback cancels its queued deliveries. A request already in flight
can still arrive, which is why receivers should treat the callback as a prompt
to call `GetTask` rather than as the authoritative task state.
