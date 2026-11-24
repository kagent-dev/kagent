# Session summaries

Kagent gives each conversation a short title and, after a break, a recap. A
control-plane worker writes both, using a model the admin picks once. This note
describes how that service works.

## Goals

- Work for every harness: Go ADK, Codex, and Claude.
- Never hold an A2A turn open, and never add a model call to the agent's own
  model traffic.
- Use one model the admin chooses once, not each agent's model.
- Decide freshness from timestamps, so the UI and backend share no turn counting.

## Setting

One install-level value names an existing `ModelConfig`. Unset turns the feature
off: the UI keeps the first-message title and shows no recap.

```yaml
config:
  sessionSummaries:
    modelConfigRef: namespace/name # empty disables titles and recaps
```

## Flow

```text
first turn settles ─► enqueue title ─┐
UI asks for recap ──► enqueue recap ─┴─► worker claims ─► reads A2A history
                                          ─► calls summary model ─► stores result
UI refetches the session ◄────────────────────────────────────────────┘
```

| Summary | Trigger | Stored with |
| --- | --- | --- |
| Title | The first turn reaches a final state | `SetSessionNameIfEmpty`, so a person's name always wins |
| Recap | The UI asks and the stored recap is stale | `SetSessionRecap`, with `generated_at` |

The title request is written in the transaction that publishes the settled task,
the same place [`publishPushBoundary`](../../go/core/internal/database/push_delivery.go)
records push work. A crash cannot lose it, and nothing runs inside the turn.

A recap is stale when it is missing, or older than the newest message in the
session's task history. Only the service compares these times. The UI shows the
recap whenever it is current, so a person who returns after a few more messages
still sees the last recap until a newer message makes it stale.

The UI asks for a recap when it detects the person coming back, using the read it
already makes to load the session. The response carries a `recap_pending` state
while the worker runs; the UI shows "Recapping…" from it and refetches.

## Worker

The worker follows the claim, lease, and retry pattern of push delivery. See
[A2A push notifications](a2a-push-notifications.md#delivery). It runs on every
replica without leader election, and PostgreSQL row claims keep one worker per
job.

- `ClaimDueSessionSummary` takes one due row with `FOR UPDATE SKIP LOCKED`, bumps
  its attempt count, and sets a claim token and lease.
- The model call runs outside the claim transaction.
- A finish call matches the claim token, so a stale worker cannot overwrite newer
  work. Failures back off like
  [`retryPushDelivery`](../../go/core/internal/database/push_delivery.go).
- Wake-ups come from a buffered, nonblocking channel after commit. A recovery scan
  finds lost hints.
- Metrics mirror
  [`worker_metrics.go`](../../go/core/internal/service/session/worker_metrics.go):
  one claim counter labeled by trigger, plus an operation name for logs.

## Data model

Names and recaps already live in the session's `data` blob (`Session.name`,
`Session.recap` in `sessions.proto`), so reads need no new fields. Work state goes in
a new side table, so the `session` table and the `session_record` view stay as
they are. A migration after `000003_session_revert_operation.sql` adds it.

| Column | Purpose |
| --- | --- |
| `session_id`, `kind` | One row per session and summary (`title`, `recap`) |
| `requested_at` | When the work was queued |
| `attempt_count`, `next_attempt_at` | Retry budget and backoff, as in push delivery |
| `claim_token`, `lease_until` | Fences expired and superseded workers |

Rows cascade with the session. A finished or exhausted job clears its row.

## Model client

The worker needs a plain text call to the chosen `ModelConfig`.

**Recommendation: import the existing clients.** Core already imports
`go/adk/pkg/models` in the translator
([`credentials.go`](../../go/core/internal/translator/credentials.go),
[`adkconfig/model.go`](../../go/core/internal/translator/adkconfig/model.go)) and
in [`discovery.go`](../../go/core/internal/service/model/discovery.go). Each
provider client there implements the ADK `LLM` interface, so one `GenerateContent`
call returns the text.

1. Resolve the `ModelConfig` the way
   [`ResolveModelConfig`](../../go/core/internal/translator/modelconfig.go) does.
2. Map it to the ADK model config, as
   [`adkconfig`](../../go/core/internal/translator/adkconfig/model.go) does for
   runtimes.
3. Build the client with `CreateLLM` in
   [`agent.go`](../../go/adk/pkg/agent/agent.go).

Two gaps remain. Today only the translator reads `apiKeySecret`, and it turns it
into a pod environment reference. The worker must read the Secret itself with the
Kubernetes client. And `CreateLLM` lives in `go/adk/pkg/agent`, which core imports
only in a test; importing it into the worker is new.

**Alternative:** move the provider clients to a shared package. It avoids core
depending on `adk/pkg/agent`, at the cost of a large move across the ADK.

The service sets its own limits, not the agent's: a small output token cap, no
thinking, and a per-call timeout.

## Bounds

| Limit | Value |
| --- | --- |
| Turns before a recap | 3 user turns |
| Transcript sent to the model | Latest 20,000 characters, thoughts excluded |
| Recap length | 2,000 characters |
| Title length | A few words, trimmed by the service |
| Attempts per job | 10, with backoff |
| Model call | Fixed timeout, set by the service |

## Security

- The "ask for a recap" read needs only read access to the session. It writes
  work state, never session content, so a share link can trigger a recap but not
  change anything else.
- Storing a title or recap needs the session's own access, checked in the store
  as `SetSessionNameIfEmpty` already does.
- Each job reads one session's history. A transcript never mixes sessions.
- The summary model's credentials stay in the control plane. Agents never see
  them.

## Failure

If the setting is empty or the model is unreachable, nothing is stored. The UI
keeps the first-message title and shows no recap. Failed jobs retry within the
budget, then retire. A later ask after retirement queues a new job.

## Public API

The service adds no RPC that sets a title or recap. The store methods
`SetSessionNameIfEmpty` and `SetSessionRecap` are internal, called only by the
worker. The "name only if empty" rule means a person's own name always wins.

## Testing

- Unit tests cover claim, lease expiry, retry, stale-claim finish, and staleness.
- An e2e test sets `config.sessionSummaries.modelConfigRef` to its own mock
  `ModelConfig`, separate from the agent's. It checks the stored title after a
  first turn and the stored recap after an ask, and that the agent's mock model
  sees no extra request.
- An e2e test leaves the setting empty and checks that no title or recap appears.

## Open questions

1. **Recap trigger.** Generate when the UI asks (this note), or once the session
   goes idle? Asking spends tokens only on chats someone returns to, but the
   person waits a few seconds. Idle is instant on return but pays for every chat.
2. **Model client.** Import the existing clients (this note), or move them to a
   shared package first?

## Implementation steps

1. Add the migration and the claim, finish, and release store methods.
2. Add the worker, its wake-up, and metrics.
3. Add the model resolver and summary client, with the service's own limits.
4. Enqueue the title when a task settles. Enqueue the recap from the session read.
5. Return `recap_pending` and the freshness-based recap to the UI.
6. Add `config.sessionSummaries.modelConfigRef` to Helm and the configuration docs.
7. Add the e2e tests.
