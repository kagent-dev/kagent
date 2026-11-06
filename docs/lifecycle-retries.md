# Retrying Session and Sandbox lifecycle requests

Session and Sandbox lifecycle mutations run within the API request. The server
records the intended operation before calling the runtime and records completion
before returning success. Sessions expose Create, Fork, Revert, and Delete; explicit
Suspend and Resume are available only for Sandboxes. Session runtime suspension and resumption
are platform-managed. A failed request can have changed the runtime: a timeout
does not mean that nothing happened, and canceling a request does not undo it.

## Client contract

- Retry Create with the **same request ID and identical input**. Keep that ID
  across client restarts. A new request ID creates a different resource. Fork
  creation follows the same rule, including its source checkpoint reference.
- Retry Delete against the same resource ID. For Sandboxes, retry Suspend and
  Resume the same way; successful calls leave the Sandbox at the requested state.
  A repeated Create returns the current resource and does not reset its lifetime
  or state.
- Serialize lifecycle mutations for each resource. Finish retrying an operation
  before issuing another. These APIs do not provide ordered, exactly-once
  commands across concurrent clients or arbitrarily delayed requests.
- Get and List only observe state. Polling them does **not** resume pending work.
  Repeat the mutation to make progress. Check both `state` and `operation` when
  reading a resource; a pending operation means the stored state may precede
  runtime effects whose completion has not yet been recorded.
- If the client stops retrying, ordinary lifecycle work can remain pending
  indefinitely. Sandbox expiration still deletes expired resources automatically.
  Session idle suspension remains an independent background action.

An executing attempt normally releases its claim when it returns. If the
apiserver crashes, another request may receive `ABORTED` until the abandoned claim
expires, at most two minutes after it was acquired. The timeout bounds an attempt,
not the total time needed for the operation. Use a fresh deadline for each retry
and an overall application deadline; retrying with an already canceled context
cannot make progress.

CLI callers should supply `--request-id` when creating a Session and retain it
for subsequent invocations. Omitting it generates a new ID on each invocation,
so rerunning that command is a new creation rather than a retry. MCP callers
should likewise retain the `request_id` passed to `create_sandbox`.

## Recovering a crashed Session

Recovery runs only when a client asks for it. Chat offers a revert or a fork from
an existing READY checkpoint after a confirmed runtime crash. Call
`SessionService.RevertSession` with the Session ID to restore its recorded runtime
to the last durable snapshot.
The caller needs Session update access. The operation verifies Substrate's CRASHED state and the recorded Actor identity.
Recovery fails interrupted turns and waiting tasks absent from the retained snapshot.
It preserves durable terminal results and returns the Session to READY with its Actor suspended.
It discards runtime changes since that snapshot and does not replay input.
A runtime without a snapshot restarts from its initial state.
`GetSession` reports `recovery_needed`; no other Session read sets it.

Retry an interrupted revert against the same Session ID; nothing retries it in the background.
Pending recovery retains its operation across API restarts, and a retry joins it.
While recovery is pending, `GetSession` reports state `FAILED`, operation `REVERT`, and failure reason `RuntimeCrashed`.
Call `RevertSession` again to finish the recovery.
An A2A send also recovers a crashed runtime: it returns `KAGENT_SEND_NOT_ACCEPTED` after reverting, so retry the send as usual.
A send while recovery is pending joins it. If the runtime cannot be recovered, the send fails with an error that is not retryable.
A retry after a revert whose response was lost returns the recovered Session.
Checkpoint creation returns `ABORTED` until it finishes.
A revert that observed the crash before another lifecycle operation began also returns `ABORTED`.
So does a revert while another lifecycle operation is active.

A revert of a runtime that is not crashed returns the Session unchanged.
A revert that can never succeed returns `FAILED_PRECONDITION`: the recorded Actor is missing, or a same-name Actor replaced it.
Stop retrying, delete the Session, then start another or fork one from a checkpoint. Delete succeeds even while the recovery is pending.
Read the reason rather than the message text, which is not stable.

## Errors

| gRPC status | Client action |
| --- | --- |
| `UNAVAILABLE`, `DEADLINE_EXCEEDED` | Retry the same mutation with capped exponential backoff and jitter. Runtime effects may already have happened. |
| `ABORTED` | Another attempt or conflicting operation may be active. Inspect current state, coordinate with other callers, then retry the intended mutation. |
| `CANCELLED` | Stop if the user canceled. If you later choose to continue, reuse the same request identity with a fresh context. |
| `INVALID_ARGUMENT`, `PERMISSION_DENIED`, `UNAUTHENTICATED`, `FAILED_PRECONDITION`, `ALREADY_EXISTS` | Correct the input, credentials, readiness, or request-ID conflict before retrying. |
| `NOT_FOUND` on Delete | No accessible live resource remains; a client ensuring absence can treat this as complete. |
| Other errors | Surface the error rather than retrying indefinitely. |

Session lifecycle also coordinates with active tasks, native cleanup, and
checkpoints. An uncertain issued Session operation must finish through retries
before a conflicting operation can proceed. Sandboxes allow a new lifecycle
intent to supersede a pending one after its active attempt ends; commands and
file transfers can be interrupted.

`DeleteSession` without `force` returns an error while the session has active
turns, without entering `DELETING`. Use `force: true` (CLI:
`kagent session delete ID --force`) to delete despite active turns. Authorization
and checks for conflicting checkpoint or lifecycle work still apply. Once deletion
is admitted, client retries do not require `force` again, including after a
preparation failure or controller restart. Both modes call `DeleteActor` directly;
suspension is not required.

For example, a client creating a sandbox should follow this sequence:

```text
request = CreateSandbox(template, request_id=persisted_uuid, ttl=1h)
delay = 250ms

until application deadline:
    result = CreateSandbox(request, fresh per-attempt deadline)
    if success:
        retain result.sandbox.id
        return result
    if status is not UNAVAILABLE, DEADLINE_EXCEEDED, or ABORTED:
        return error
    wait random duration between 0 and delay
    delay = min(delay * 2, 5s)

report incomplete; retain request for a later retry
```

## Guest operations have a different contract

This guidance applies to lifecycle mutations. **Do not automatically retry
StartProcess:** every call starts a new command, and a lost response may mean
that command is already running. Process and file services use env's upstream
semantics. An interrupted WriteFile can leave partial data, so callers must
decide whether rewriting the destination is appropriate.
