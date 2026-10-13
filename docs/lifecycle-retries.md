# Retrying Session and Sandbox lifecycle requests

Session and Sandbox lifecycle mutations run within the API request. The server
records the intended operation before calling the runtime and records completion
before returning success. A failed request can have changed the runtime: a timeout
does not mean that nothing happened, and canceling a request does not undo it.

## Client contract

- Retry Create with the **same request ID and identical input**. Keep that ID
  across client restarts. A new request ID creates a different resource. Fork
  creation follows the same rule, including its source checkpoint reference.
- Retry Suspend, Resume, or Delete against the same resource ID. Successful
  Suspend/Resume calls leave the resource at the requested state. A repeated
  Create returns the current resource and does not reset its lifetime or state.
- Serialize lifecycle mutations for each resource. Finish retrying an operation
  before issuing its opposite. These APIs do not provide ordered, exactly-once
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
