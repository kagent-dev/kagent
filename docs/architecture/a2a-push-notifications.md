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

A callback receives `statusUpdate` notifications after its task enters
`INPUT_REQUIRED`, `AUTH_REQUIRED`, `COMPLETED`, `FAILED`, `CANCELED`, or
`REJECTED`. It does not receive earlier states or ordinary progress updates.
Pending notifications are coalesced: a newer eligible published state replaces
an older pending notification and its retries. Resuming execution clears an
obsolete waiting notification without sending a progress notification. A resume
means the runtime successfully saves an active task state; reserving dispatch or
saving input while still waiting does not clear the notice. Task event history
is retained. Creating or changing a callback does not replay an earlier state.

## Delivery

Each callback registration holds at most one pending notification. Kagent
replaces that pending state in the same database transaction that publishes the
task state, and clears waiting notifications atomically with active task saves.
Each API replica runs a sender with up to ten concurrent deliveries by default,
woken through its own buffered, nonblocking channel after settlement commits.
Push and quiescence have separate channels so neither consumes the other's hint.
PostgreSQL row claims, leases, and claim tokens coordinate delivery across
replicas; HTTP runs outside the claim transaction.

Configure `push.maxConcurrency` (positive integer, default `10`) and
`push.sendTimeout` (positive duration, default `5s`) in the controller config.
With Helm, set these under `config.push`:

```yaml
config:
  push:
    maxConcurrency: 10
    sendTimeout: 5s
```

The concurrency limit applies per replica, across its callback destinations.
Setting it to `1` selects serial delivery. Each freed slot immediately accepts
another delivery, even while other requests remain slow. Registrations are only
claimed when capacity is available. Leases cover the configured send timeout plus
30 seconds for preparation and persistence. Preparation errors and send timeouts
record failed attempts for retry. Shutdown cancels active sends and allows up to
five seconds to persist each outcome. Successful sends remain acknowledged;
shutdown-interrupted attempts release their claims immediately without consuming
an attempt. Claims whose outcomes could not be saved recover after lease expiry.

Workers claim pending registrations at startup and after wake-ups. When idle,
recovery scans run once a minute by default (`push.recoveryInterval`, which must be
positive). Unbound registrations are cleaned up at startup and on a separate timer
at that interval, including when all delivery slots are occupied. Lost hints,
including a crash between commit and signaling, can delay delivery until another
replica's next scan. Outstanding retries and leased deliveries use a five-second polling interval,
and database errors use one-second retries. Each pass claims at most 100
deliveries, bounded by available capacity; a full batch continues immediately.
Send completion and local hints also wake the sender. Queued completions are
drained together before refilling capacity. Saturated senders wait for capacity
without querying for pending deliveries.

Each notification generation permits up to 20 attempts across controller restarts
and replicas. Preparation and HTTP failures back off for 5s, 10s, 20s, 40s, then
cap at 60s: about 16 minutes of backoff, plus request and scheduling time.
Exhaustion clears pending work but keeps the registration; a new publication
resets the attempt count and backoff.
An acknowledgement or failure from an older generation cannot change newer
pending work. Workers capture the latest published task status when claiming and
check that the claim is still current immediately before sending. Requests
already in flight can become stale when execution resumes or a newer state is
published.
Requests can arrive more than once or out of order, so receivers
should handle duplicates and use `GetTask` as the source of truth. Any 2xx
response acknowledges delivery. Redirects are treated as failures; register
the final callback URL. Kagent does not expose delivery status.

Kagent sends a JSON A2A `StreamResponse` containing a `statusUpdate` with the
task ID, context ID, and latest published status, including any status message.
Unpublished waiting or terminal states are withheld until native cleanup settles
them. Ordinary progress updates do not trigger callbacks. Call authenticated
`GetTask` for the current state, artifacts, and message history.

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
`authentication.credentials` fields. Push notifications work without a signing
key and are sent without an `Authorization` header by default.

To enable JWT authentication, generate an Ed25519 key once and store its
unencrypted PKCS#8 PEM in an operator-managed Kubernetes Secret:

```sh
openssl genpkey -algorithm ED25519 -out private-key.pem
kubectl -n kagent create secret generic push-signing --from-file=private-key.pem
```

Set `controller.push.signing.secretName: push-signing` to mount `private-key.pem`.
For CSI/custom mounts, set `config.push.signingPrivateKeyFile` and supply
`controller.volumes` and `controller.volumeMounts`; `secretName` overrides the
path. Outside Helm, use `push.signingPrivateKeyFile`. All replicas need the same
key. Invalid keys prevent startup.

When a key is configured, Kagent sends a fresh five-minute Ed25519 JWT as
`Authorization: Bearer <JWT>` on each attempt and publishes the public key at
`/.well-known/jwks.json`. Without a key, that endpoint returns 404.
Webhook receivers need the configured issuer and a trusted, reachable JWKS URL
(on the controller or UI). Verify the JWT's EdDSA signature using its `kid`, then
check `iss`, `iat`, `nbf`, `exp`, `taskId`, and `aud` against the exact callback
URL, including the query string. Each attempt has a fresh `jti`. Acknowledge
accepted callbacks promptly with 2xx; use your own A2A credentials to call
`GetTask`.

Helm chooses the issuer from `config.push.issuer`,
`ui.externalUrl`, then the in-cluster gateway URL; outside Helm,
`push.issuer` defaults to `gateway.publicUrl`. Keep the issuer stable.
Each replica loads the issuer’s public keys from PostgreSQL at startup, including
keys absent from its mounts. It refreshes this snapshot as it polls the current
key file every 12–18s. JWKS requests read the snapshot from memory.
The replica keeps its last valid key and publishes each replacement in JWKS for
`config.push.signingKeyActivationDelay` (default `300s`, maximum `86400s`) plus
30s before signing with it. Set the delay to cover the longest receiver JWKS
refresh interval.

Changing a signing Secret name rolls the controller. For a handoff, set
`controller.push.signing.secretName` to the new Secret and
`controller.push.signing.previousSecretName` to the old one. Both use `private-key.pem`.
For CSI or custom mounts, use `config.push.previousSigningPrivateKeyFile` to point
to a copy of the old key before replacing the current file. Previous is read at startup.
With previous configured, a replica keeps publishing its outgoing public key until its next handoff.

If current differs from the latest activation in the database, a starting
replica with a distinct previous key signs with previous for a fresh delay.
Once current is recorded, startups use it immediately and running replicas
finish their own delays. If previous is omitted
or identical to current, startup uses current immediately and preserves any existing
activation record. If a configured previous file is missing, startup fails unless
current is already recorded, in which case the replica logs a warning and uses current.

A key stays published while any replica renews it and through the expiry of its
last JWT plus 30s. If database refreshes fail, replicas serve their last
snapshot and pause signing once they can no longer cover a new JWT’s lifetime.
A replica restarts a pending key’s delay if its confirmed publication expires.

To roll back, swap the key references. Callbacks retry on authentication failures.
If B was active before an A-only rollback, retrying B skips the delay, so receivers caching only A may reject callbacks until they refresh JWKS.

HTTPS and public destinations are required by default. Operators can enable
HTTP and private, loopback, or link-local destinations with
`config.push.allowHttp` and
`config.push.allowPrivateNetworks`. These exceptions are
needed for some in-cluster test receivers; only enable them for trusted
networks. The sender checks resolved addresses at connection time and does not
follow redirects.
