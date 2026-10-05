# A2A push notifications

Kagent can notify a webhook when an A2A task needs input or reaches a final
state. The Agent Card advertises `capabilities.pushNotifications: true`.
Callbacks belong to one task; they do not follow other tasks in the same
conversation.

## Register a callback

Supply a callback URL in `taskPushNotificationConfig` with `SendMessage` or
`SendStreamingMessage`, or call `CreateTaskPushNotificationConfig` for an active
task. An embedded config must leave `taskId` empty. Kagent records it before
dispatch and attaches it only when a task write accepts that message. Create
already identifies the task. Get, List, and Delete manage registered callbacks.

```json
{
  "jsonrpc": "2.0",
  "id": "request-1",
  "method": "SendMessage",
  "params": {
    "message": {
      "messageId": "message-1",
      "role": "ROLE_USER",
      "parts": [{ "text": "Hello" }]
    },
    "configuration": {
      "returnImmediately": true,
      "taskPushNotificationConfig": {
        "url": "https://receiver.example/callback"
      }
    }
  }
}
```

The CLI supports `kagent agent invoke --session SESSION_ID --task TEXT
--push-url URL`. For an existing task, use `kagent agent session push create
SESSION_ID TASK_ID --url URL` and the corresponding get, list, and delete
commands.

A callback receives future `statusUpdate` events when its task enters
`INPUT_REQUIRED`, `AUTH_REQUIRED`, `COMPLETED`, `FAILED`, `CANCELED`, or
`REJECTED`. It does not receive earlier states or ordinary progress updates.
A receiver should call authenticated `GetTask` for the current state and
artifacts.

## Delivery

Kagent queues eligible updates in the same database transaction that publishes
the task state. A leader-elected worker sends them after that transaction
commits. Failed attempts retry up to ten times, including after controller
restarts. Requests can arrive more than once or out of order, so receivers
should handle duplicates and use `GetTask` as the source of truth. Any 2xx
response acknowledges delivery. Kagent does not expose delivery status.

Kagent sends a JSON A2A `statusUpdate` for `INPUT_REQUIRED`, `AUTH_REQUIRED`, or
a terminal state (`COMPLETED`, `FAILED`, `CANCELED`, or `REJECTED`). The update
contains the task ID and status; it may contain a status message. Treat it as a
signal to call authenticated `GetTask` for the current result and artifacts.
Ordinary progress updates do not trigger callbacks.

For example, the webhook receives this JSON body when a task completes:

```json
{
  "statusUpdate": {
    "taskId": "task-123",
    "contextId": "context-456",
    "status": { "state": "TASK_STATE_COMPLETED" }
  }
}
```

## Authentication and Security

Register only the callback URL. Kagent rejects nonempty `token` or
`authentication.credentials` fields and sends a fresh five-minute Ed25519 JWT
as `Authorization: Bearer <JWT>` on each attempt.

Webhook receivers need the configured issuer and a trusted, reachable JWKS URL
(`/.well-known/jwks.json` on the controller or UI). Verify the JWT's EdDSA
signature using its `kid`, then check `iss`, `iat`, `nbf`, `exp`, `taskId`, and
`aud` against the exact callback URL, including the query string. Each
attempt has a fresh `jti`. Acknowledge accepted callbacks promptly with 2xx;
use your own A2A credentials to call `GetTask`.

Helm creates and retains a shared signing Secret. For an existing Secret, set
`controller.push.signing.existingSecret` to one with a `seed` key containing a
base64-encoded 32-byte Ed25519 seed; outside Helm, set
`KAGENT_A2A_PUSH_SIGNING_SEED`. All controller replicas need the same seed.
Tell receivers the issuer and JWKS URL. Helm chooses the issuer from
`controller.push.signing.issuer`, `ui.externalUrl`, then the in-cluster gateway
URL; outside Helm, `KAGENT_A2A_PUSH_ISSUER` overrides `KAGENT_GATEWAY_URL`.
Keep the issuer stable. Seed rotation immediately replaces the published key;
restart Pods after changing an operator-managed Secret.

HTTPS and public destinations are required by default. Operators can enable
HTTP and private, loopback, or link-local destinations with
`controller.push.allowHTTP` and `controller.push.allowPrivateNetworks` (or the
corresponding `KAGENT_A2A_PUSH_ALLOW_HTTP` and
`KAGENT_A2A_PUSH_ALLOW_PRIVATE_NETWORKS` variables). These exceptions are
needed for some in-cluster test receivers; only enable them for trusted
networks. The sender checks resolved addresses at connection time and on
redirects.
