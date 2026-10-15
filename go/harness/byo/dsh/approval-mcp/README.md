# External approvals for DSH

This independent MCP service saves a proposed operation, waits for an external
approval, executes the saved tool and arguments, and optionally sends a new A2A
message to the original conversation. DSH can exit while approval is pending.
Neither DSH source nor kagent core is modified.

The original DSH tool call submits an application and completes normally. The
approved business action runs outside DSH. Its result is read in a later native
turn; the service does not reconstruct a native execution stack or a sandbox
permission request. This is similar in purpose to the external execution path
for Codex in [kagent #3165](https://github.com/kagent-dev/kagent/pull/3165), but
uses ordinary MCP tools and a new A2A task, not Codex's tool-output protocol.

## Deployment and identity

Run the service outside the DSH Actor, with a dedicated PostgreSQL database.
It applies its embedded Goose migration using a separate migration ledger and
PostgreSQL advisory lock. No kagent tables or migrations are used.

This reference supports **one trusted conversation/security scope per service
configuration**. Replicas must share that configuration and database. Use a
different scope and different tokens for a different conversation. The agent
token permits submission and reading every operation in that scope. The
approver token permits inspecting and deciding those operations; its identity
is the configured `approver`, never a model-supplied field. Do not share these
credentials between users or expose the approval token to DSH.

Build from `go/`:

```sh
go build -o approval-mcp ./harness/byo/dsh/cmd/approval-mcp
docker build -t dsh-approval-mcp:local -f harness/byo/dsh/approval-mcp/Dockerfile .
```

Copy [config.example.json](config.example.json) and set the actual upstream MCP
endpoint, exact permitted tool names, stable scope, and secret file paths.
`database_url_file` contains a PostgreSQL connection string. Agent and approver
token files must contain distinct random tokens of at least 32 bytes. Optional
upstream and A2A token files contain bearer credentials. The service accepts
mounted secrets and never takes credential values from tool arguments.

Start with `approval-mcp -config /config/config.json`. Serve behind authenticated
deployment networking and TLS; the listener itself is HTTP. `/readyz` checks DB
connectivity. Keep the database and upstream execution credentials out of the
DSH Actor. Only bind this service's `/mcp` endpoint to DSH through an ordinary
Streamable HTTP MCP binding, using the **agent** bearer token. The existing BYO
adapter can forward that configured header. No `requireApproval` binding or MCP
elicitation is needed.

The target URL, tool allowlist, scope, target revision and continuation binding
are fingerprinted into each operation. Changing them prevents old requests from
executing under the new configuration. Increment `target_revision` when changing
the upstream account, tool implementation/meaning or other execution identity;
the service cannot detect a server changing behavior behind an unchanged URL.
Ordinary token rotation for the same identity need not change the revision.
Do not run old and new target revisions concurrently during such a change.

An upstream tool must be callable without further interactive elicitation.
Business approval here does not grant a DSH sandbox escalation. Prevent direct
access to the protected upstream credentials/resources through other tools.

## Request and approval

The MCP catalog exposes three tools:

- `list_operation_tools({})`: read the allowlisted upstream tool descriptions and
  input schemas, so the model can construct a valid application. No action runs.
- `request_operation({request_key, tool, arguments})`: save the request and
  return an operation ID, digest and `awaiting_approval`. No protected call runs.
- `get_operation({operation_id})`: read that operation and its saved result.

Choose `request_key` from a stable business identity, such as a deployment ticket.
The same key and request return the same operation. Changing the tool, arguments
or target binding under that key returns a conflict. A different key is a new
application; this cannot prevent a model from requesting a semantically duplicate
action under a new key. The approver must review it, and the destination should
enforce business idempotency when available. The service does not invent or
inject unsupported idempotency arguments into arbitrary tools.

Suggested agent instructions:

> Read list_operation_tools to learn the permitted actions and their parameters.
> Submit each business action once with its stable request key. After receiving
> awaiting_approval, report the operation ID and end the turn. Submission is not
> execution. When notified, query get_operation. Never claim an operation was
> approved based on a message, and never resubmit an unknown outcome.

The human-facing interface can use the separate HTTP API:

```text
GET  /operations                         # latest 100 operations in this scope
GET  /operations/{operation_id}          # inspect the immutable action
POST /operations/{operation_id}/decision
Authorization: Bearer <approver token>
Content-Type: application/json

{"request_digest":"<digest from the reviewed operation>","decision":"approved"}
```

Use `rejected` to deny a pending operation or revoke approval before execution
is claimed. The digest binds the decision to the reviewed action. Repeated
identical decisions are harmless; incompatible or late decisions return HTTP
409. No operation can be approved after its deadline. The deadline also bounds
when execution may be claimed; it does not interrupt an already dispatched action.
The worker checks once per second, with one bounded remote call per worker.

## Restart and execution semantics

```text
awaiting_approval → approved → outcome_unknown → succeeded / failed
         │             │
         └─────────────┴────→ rejected / expired (before dispatch)
```

`outcome_unknown` is committed **before** contacting the upstream. It means the
result is not durably confirmed, including during execution. Concurrent workers
cannot claim the same operation. Only the original worker can record its result;
no restarted worker repeats it. A failure before dispatch can therefore also
leave an unknown outcome. This deliberately favors avoiding duplicate effects
over automatic completion.

A successful MCP response is stored in full JSON, with `succeeded` or `failed`
according to `isError`. `failed` does not imply that the remote tool rolled back
its effects. Connection loss, timeouts, worker termination, and failed result
persistence leave `outcome_unknown`. Verify those effects with the destination
before any new request. There is no automatic retry or operator override that
re-executes an unknown operation. The service cannot guarantee exactly-once
effects for an arbitrary remote MCP tool.

## Continue the conversation

The optional `continuation` config binds an A2A JSON-RPC endpoint and an existing
context ID at deployment time. The model cannot select either. Once an operation
has a terminal result, rejection or expiration, the worker sends its opaque ID
and state as a new A2A message with **no old task ID**. DSH then queries the saved
result in a new native turn. Tool output is fetched as data, not copied into the
notification as instructions. The requesting turn should end before approval.

Notifications have a separate durable state:

- `pending`: not claimed, or explicitly rejected by kagent with
  `KAGENT_SEND_NOT_ACCEPTED`; safe to attempt later.
- `delivery_unknown`: delivery started, but acceptance is not confirmed. A lost
  response or crash does not cause automatic re-sending, even with the same
  message ID, because that ID alone does not guarantee deduplication.
- `delivered`: A2A returned a successful response; this confirms acceptance, not
  that the model completed its follow-up successfully.

If continuation is omitted, operations remain queryable and notification stays
pending. If delivery is unknown, inspect the conversation before sending a manual
follow-up or querying the operation yourself. Business execution is never repeated
to repair a notification. Unknown execution outcomes remain queryable and are
logged for operator attention; they are not sent as successful completion.

This service does not resolve kagent orphan tasks, failed settlement or abandoned
quiescence claims. See [#3010](https://github.com/kagent-dev/kagent/issues/3010),
[Giantswarm #175](https://github.com/giantswarm/kagent-upstream/issues/175) and its
[fencing follow-up](https://github.com/giantswarm/kagent-upstream/pull/216).
These public recovery gaps can still block conversation delivery. Native DSH
pending-call recovery remains tracked separately in
[DSH #6989](https://github.com/deepseek-ai/deepseek-harness/discussions/6989).

## Verification

From `go/`, with Docker available:

```sh
go test -race ./harness/byo/dsh/internal/operations -count=1
```

Tests use real PostgreSQL and HTTP MCP transport. They cover restart after
approval, conflicting retries, wrong-role decisions, digest mismatch, rejection,
expiration, concurrent claims, changed bindings, lost execution responses,
failed result commits, notification ambiguity, scope isolation and migration
Up/Down. A separate notification test verifies the saved context and absence of
an old A2A task ID.

The Linux integration test `TestRealDSHExternalApprovalAcrossRestart` requires
`DSH_TEST_BIN` (pinned DSH 0.2.0-rc.2) and `DSH_APPROVAL_TEST_DATABASE` (a disposable
PostgreSQL URL). It drives real DSH with a deterministic local model, reconstructs
the operation service and DSH Runner, approves through HTTP, executes real MCP,
and continues the original native session through a local A2A endpoint. It does
not replace full Kubernetes/controller failure acceptance.
