# Deferred specs

Coverage this suite does not have, and what each gap is waiting on. Kept as prose
rather than as skipped tests, because a skipped spec reads as coverage and this list
does not.

An entry is in one of three states, and saying which is most of the value here:
**deferred** (blocked on something nameable), **not planned** (a decision, so it is not
re-argued every time somebody notices the gap), or **closed**.

Three rules for editing it, all learned the hard way here:

- **A stale entry costs more than no entry.** Several entries were once listed as
  blocked on pages that already existed, which stops somebody porting work that is
  already possible. When something lands, close it in the same change.
- **Close, do not archive.** An entry describing a page that no longer exists, or a
  gap since covered, belongs in *Closed* below as one line — or deleted.
- **Do not defer a decision.** If the suite is not going to cover something, say so and
  say why. An entry that reads as queued is one somebody will pick up.

## Closed

Nothing below is a gap. They are listed only so they are not looked for again.

- **Onboarding.** There is no onboarding wizard on this architecture and none planned.
- **A cleanup spec.** Each mock test gets a fresh browser context, and each live spec
  deletes what it made in a `test.afterEach`. A run killed outright still leaks, which
  is why `throwawayName` puts the process and a timestamp in every name: anything
  matching `e2e-live-*` in `kagent` is litter and safe to remove by hand. A sweep spec
  stays the wrong shape for that, being one bad selector away from deleting real work.
- **Agent-create validation.** There is no agent form. An agent is an `AgentTemplate`
  paired with a `Harness`, materialised by admission — `router/routes.ts` records why
  there is no `agentNew` and no `agentEdit`. Nothing creates one, so there is nothing
  to validate. The create-and-read-back property that lived in the removed agent-create
  spec is now `tests/harnesses/harnesses.spec.ts`.
- **The REST path tests.** `src/api/{readPaths,writePaths}.test.ts` drove the client over
  REST URLs that no longer exist; the controller serves gRPC-Web. `src/api/operations.test.ts`
  replaces them against the real generated descriptors and covers strictly more.
- **The extension-point specs.** `extension-points-absent.spec.ts` and
  `extension-points.withExtension.spec.ts` both run and between them assert every point
  the app declares.
- **Tool approval, and a question asked without the extension.** Both shipped with #2714
  and are driven by `tests/chat/approvals.spec.ts` — several tools decided independently
  behind a Submit, one tool decided on the prompt itself, and the unanswerable turn that
  says so rather than inventing controls. The decisions are read back off the reply, not
  off the form.
- **`AgentDetailsPage`.** Covered by `tests/agents/agent-details.spec.ts`: the state and
  what it means, the links out to the template and the agent, the failure, and the
  difference between a record that is missing and a read that failed. The page used to
  show a `SandboxAgent`'s spec — its model, its tool bindings, its `Ready` condition —
  and shows an `AgentInstance` record instead. That is not a reduction to restore: an
  instance genuinely has no spec, and the configuration lives on the `AgentTemplate` and
  `Harness` surfaces, which have their own specs.
- **The live suite running anywhere.** `playwright/live/` runs in the `test-e2e` job
  against the image built from `ui/Dockerfile`. `README.md` has the wiring.
- **Chat and its error journeys, the chat-message extension point, MCP servers in mock,
  prompt libraries, and form validation for every resource with a form.** Each is a spec
  now, and a spec describes itself better than a list of what it covers.

## Deferred: MCP servers stay mock-only — the list cannot read its own writes

Every other resource has its create/read/change/delete journey in `shared/`, running
against both backends. MCP servers do not, and the reason is a defect rather than an
awkward fixture: **#2849**.

The page's two halves use different stores. `CreateToolServer` writes a Kubernetes
`RemoteMCPServer`; `ListToolServers` reads the PostgreSQL `toolserver` table; the only
writer of that table is the reconciler, after it has tried to connect to the server. So
the list lags a create by however long discovery takes. Measured on a cluster:

```
CreateToolServer   OK
ListToolServers    OK   <- 57ms later; the new server is not in it
   (nothing further)
ListToolServers    OK   <- a fresh page load a minute on; now present
```

Delete has the same shape in reverse, and `DeleteToolServer` resolves the server's kind
from that same projection — so while a new server is invisible it is also undeletable.

A shared spec was written and did pass, by pressing **Refresh** after the create and
after the delete. It was withdrawn rather than landed: a spec that presses through a
defect to stay green is how the defect stops being noticed, and the press would have
needed removing anyway. The mock lifecycle in `tests/mcp-servers/mcp-servers.spec.ts`
keeps its full coverage meanwhile.

**Revisit when #2849 lands.** The spec is a short port of the mock one — create with a
URL the cluster can resolve, read the row back, delete it — and the acceptance test is
that it needs no Refresh.

Worth recording for its own sake: the fixtures cannot show this class of bug at all. They
answer from the page's own memory and are therefore always immediately consistent, so a
mock backend has no write that is not yet a read. Only a cluster has one.

## Not planned: the `resuming` and `suspending` lifecycle stages

A decision rather than a queue entry, recorded so it is not re-argued each time
somebody notices the gap.

`chat.spec.ts` drives the lifecycle indicator at rest and through `running`, because a
turn produces both. The other two stages come from `AgentInstance.operation`, which the
controller claims and clears as it works. The mock backend serves a static record, and
faking one would prove only that a fixture can hold a string; a live journey — suspend an
instance from the agents list with a chat page open on it, and watch the indicator follow
— needs a model that can answer, which neither cluster this suite runs against has.

**What tips it from deferred to not planned** is that the part with the logic in it is
already covered, and it is covered where the logic lives:
`src/components/chat/lifecycleReading.test.ts` exercises every reading exhaustively,
including the case worth guarding hardest — that **no stage is claimed when a turn ends**,
since a substrate agent really does suspend itself then and nothing in the API reports it.
What a browser test would add is that a string the controller sets reaches an element,
for two stages out of four, at the cost of the only spec in this suite needing its own
API key.

Revisit if the indicator grows behaviour of its own, rather than reading a field.

## Deferred: streaming, end to end

The one chat gap still worth a spec, and it needs a cluster with a model that answers:
CI installs with `OPENAI_API_KEY: fake` and `setup-cluster.sh` sets no key at all, so it
is reachable today only by a developer with their own.

The client honours an artifact's `append` flag, which is how this runtime streams: one
`artifactId` for the reply, one frame per token, `append` on every frame after the
first, then a closing frame repeating the whole answer. That shape
is pinned in `src/api/chat/a2aGrpcChatClient.test.ts` against frames captured from the
controller on 2026-08-24. The mock chat client streams `delta` events — the port's
vocabulary rather than the wire's — so no browser test exercises the artifact path.
Teaching the fixture to emit artifact frames would make it an A2A server rather than a
`ChatClient`, which is the wrong seam; the transport is already covered over real bytes.
The gap is a live spec that sends a message and watches the reply grow before the turn
completes.

## Open, but a product decision rather than coverage

The `ask_user` payload still renders as JSON in the transcript beside the answerable
prompt. That is duplication rather than a defect, and collapsing it needs a decision
about whether a tool call with an interactive rendering should show its raw form at all.

## Deferred: proving a share token actually travels

`tests/chat/sharing.spec.ts` covers the loop — create a link, see it listed once, revoke
it, open one — and can prove the page spends a token and reports a refusal. It cannot
prove the header reaches a backend: chat in mock mode is served by a client-side fake
that builds no request, so the spec reads the registration directly rather than seeing
what travelled.

Only a live spec closes it, and unlike the two chat gaps above this one needs no model
that answers — a share is over an instance, and an instance exists as soon as a
conversation is opened. What it needs is a second identity: the point of the check is
that the A2A gateway reads the instance as the share's *owner*, which a visitor who is
the same signed-in reader cannot demonstrate.

## Blocked on the API: server-side paging, searching and sorting — for every list

**Several lists still narrow their rows in the browser because their RPCs return whole lists.**
Recorded here rather than left implicit, because the shape of the request is the whole
argument: a client-side filter is honest when the response holds every row and dishonest
when it holds one page of them, and only the proto says which.

| Read | Request today | What it takes | What it needs |
|---|---|---|---|
| `ListModelConfigs` | `ListModelConfigsRequest {}` | nothing at all | `PageRequest page`, `string filter`, a sort field enum and `SortOrder` |
| `ListToolServers` | `ListToolServersRequest {}` | nothing at all | the same four |
| `ListPromptTemplates` | `ListPromptTemplatesRequest { string namespace = 1 }` | one namespace | `PageRequest page`, `string filter`, sort field and order — the namespace is already there |
| `ListSubstrateActors` | `ListSubstrateActorsRequest { atespace, page }` | a page | `string filter` and a sort field enum — **and ate-api has to grow them first** |
| `ListSubstrateWorkers` | `ListSubstrateWorkersRequest { namespace, page }` | a page | the same two, on the same condition |

**Substrate actor and worker lists use upstream pagination.** Each list request makes
one ate-api call and preserves its order and continuation token. Actors use upstream
atespace filtering. Worker namespace filtering applies only to the returned page, so an
empty worker page may still have a next token. The UI labels counts as "on this page"
and offers no actor/worker text search or column sorting.

Two capabilities remain deferred until Substrate supports them:

- Global sorting and text search require upstream list-query support.
- Exact totals require upstream aggregates. `GetSubstrateSummary` still walks every
  page to compute the dashboard counts; actor and worker page responses have no totals.

**Which actor is on a worker is not deferred; it is not available.** ate-api's `Worker`
carries capacity and allocation and no actor reference — the binding lives on the actor —
so the workers table has no Actor column. `busyWorkerCount` counts workers with a positive
allocated actor count reported by Substrate. A column would need the walk per page.

**A single-message read is defensible only while the message really holds everything.**
`GetSubstrateStatus` is the read that failed this way once: a cluster of 410,110 actors
produces a response gRPC refused to send, which is why the substrate page was split into
three reads in the first place. That endpoint has been removed from `SystemService`. For
the three reads at the top of this table that do still answer with everything, **the
moment one starts paging — or starts truncating to survive — its client-side search and
sort must be labelled or removed in the same change**, because an unlabelled filter over
a page reports "no matches" about a row on page nine.

The prompts page is a partial exception worth not losing: `ListPromptTemplates` takes a
namespace, so `usePrompts` fans out one call per namespace and its **namespace filter is
genuinely server-side already**. Only its search and sort are not.

### Not deferred, but named here so it is not looked for: paging is client-side too

Every one of these tables shows a page control, and for the model, tool and prompt lists
it pages rows that are already in the browser — a real convenience on a long list, and
not a claim about the server. The totals beside those controls are therefore true
totals, which is only true because those reads return everything.

The substrate tables are the exception and now the model: their page control turns real
pages, and none of their totals is `rows.length`. Counting what arrived and calling it a
total is the lie a separate summary read exists to prevent, which is what
`GetSubstrateSummary` is for.

## Not deferred coverage: auto-titling costs a read per row

A cost decision with a server-side fix, rather than a spec somebody owes. What the
table renders today is pinned by `agents/agent-page.spec.ts` — that it is never a bare
UUID, and that the derived title appears where the transcript is in hand — so the
behaviour is covered; what is open is making a better behaviour possible.

A conversation is named by the reader, and an unnamed one can be titled from its first
message — `ListTasks{ContextID: instanceId}` returns the history. That is **free on the
chat page**, which has already read the transcript because it is rendering it.

The **rail** now pays for the rest, bounded at thirty: every row but the open one used to
read `Untitled · 50b46891`, which made the list very nearly unusable. Failures are per-row
and silent, because a title is a convenience over an id that already identifies the row.

The agent's conversation **table** still falls back to `Untitled · <short id>`, and that
is a decision rather than an omission: it is the surface that could hold hundreds of
rows, and one read per row to put a label on them is the cost the rail's budget of thirty
exists to bound.

Two ways it could stop being a trade-off, both server-side and neither invented here:

- **`AgentInstance` carries the first message**, denormalised the way `description` and
  `model_config_ref` already are on `AgentTemplate`. One extra string on a message the
  list returns anyway, and no extra call at all.
- **`ListAgentInstances` gains a field mask** for it, so callers that want it pay and
  callers that do not are unaffected.

Neither has landed, checked at the source rather than here: `AgentInstance` in
`proto/kagent/api/v1alpha1/agent_instances.proto` gained `name` (field 13, the
reader-supplied title) and `context_id`, and carries nothing derived from the
transcript.

## Not a defect yet: an agent's conversation search is over what was fetched

A tripwire rather than a gap, and the distinction is the whole entry: the search is
honest today and stops being honest on a change somebody will make for other reasons.

`ListAgentInstances` narrows to one agent **on the server**: it takes `agent_template`
and `harness` and resolves them through the prepared revision. That is the narrowing
that matters, because it is the one the paging is applied after. What the request does
**not** carry is a search term or a sort field — `ListAgentInstancesRequest` is
`all_creators`, `page`, `agent_template` and `harness`, and nothing else — so the agent
page's search box and column sorts run in the browser.

That is honest here for a reason worth stating, because it is the one read on the list
above that is paged at all: the client follows every page token before rendering anything
(`INSTANCE_PAGE_LIMIT` in `api/grpc/operations.ts`), so what is in the browser is every
conversation with that agent rather than the first fifty.

**If that page-following is ever removed** — and it should be, once an agent can have
thousands of conversations — the search and the sort must go server-side in the same
change. The fields to add are the four in the table above, and there is no longer an RPC
in this repository carrying them to copy from.

## An agent's page is derived, because a pair is not an object

`/agents/:namespace/:agentTemplate/on/:harness` reads a template and filters
conversations; there is no `GetAgentPair` because there is no pair *service*. A pair is
derived — the controller materialises it from admission and retires it when the labels
stop matching — so nothing creates one and nothing could name one.

Two consequences are visible on screen and are deliberate. An agent cannot be renamed,
so two agents cut from one template share a name and are told apart by the harness
column. And an agent's page cannot show a revision history, a creation time, or who made
it: `agent_template_harness_pair` holds all three and no RPC exposes the table. Adding
one is the change that would unblock both, and it is a larger decision than this
surface.

## A new template labelled for the only harness

**What is not covered:** that a new agent template arrives already labelled for the
harness that will run it, when the cluster has exactly one.

**Why:** the fixtures carry more than one harness on purpose — one of them exists
specifically so a template can be admitted by *two*, which is what makes an agent list
show two rows for one template. A single-harness cluster is therefore not a state these
fixtures can be in, and the default correctly does nothing against them. The opposite
half *is* covered: with several harnesses nothing is chosen for the reader, and a
template no harness admits says so.

**How it was checked instead:** against the live cluster, which has one harness
(`kagent`) — the same shape the default exists for.

**What would close it:** a fixture scenario with a single harness. Worth doing when
something else needs one; a scenario knob added for one assertion is a second fixture
backend to keep honest.
