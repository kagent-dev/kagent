# Deferred specs

The old suite had 13 specs. Everything it covered that still has a page is ported,
plus a good deal it did not have: a lifecycle spec per resource, routing, auth, the
two extension-point specs. The rest is listed here rather than committed as skipped
tests, because a skipped or vacuous spec reads as coverage and this list does not.

Where an entry below names a spec that no longer exists under that filename, the
coverage moved rather than went; `README.md` has the current layout.

Each entry names the surface that has to exist before the spec can assert
anything real. In every case the data layer is already in place — what is
missing is the page.

| Old spec | Blocked on | Already available |
|---|---|---|
| `onboarding/onboarding.spec.ts` | No onboarding wizard exists on this architecture | — nothing; drop it unless the flow is rebuilt |
| `cleanup.spec.ts` | Still not a spec. The mock suite gives each test a fresh browser context, so there is nothing to sweep; the live suite creates real resources and each spec removes what it made in a `test.afterEach`. What neither covers is a run *killed* between the two — see below. | — |

## Now running in CI: the live suite

`playwright/live/` runs in the `test-e2e` job, against the image built from
`ui/Dockerfile` on the same cluster as the Go end-to-end tests. `README.md` has the
wiring. Two things follow for this file.

**The cleanup entry above is now about litter, not contexts.** A live spec deletes what
it created, in a `test.afterEach` rather than a `finally` — measured, a timed-out test has
a closed page and everything in its `finally` throws, which is the failure most likely to
leave something behind. A run killed outright still cannot. That is why `throwawayName` puts the
process and a timestamp in every name — anything matching `e2e-live-*` in `kagent` is
litter and safe to remove. A sweep spec stays the wrong shape for it, being one bad
selector away from deleting somebody's work.

**Every live spec was broken the first time one was run against a cluster**, each in a
way the mock suite structurally could not show: `agent-lifecycle.spec.ts` — since
replaced by `shared/agent-templates/` — drove
`/agents/new`, a page long removed, for an agent nobody creates; `schedules.spec.ts`
drove a dialog for an editor that is a page, and named CI's fixture rather than
`setup-cluster.sh`'s; `substrate.spec.ts` read a tile once, catching the em-dash it
draws before the data lands. Two harness faults came out of the same run and were fixed
in `helpers/resource.ts`.

The specs were written, they were correct, nothing ran them, and they decayed into files
testing pages that had been removed. A stale entry costs more than no entry; a stale
*spec* costs more again.

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

## Ported since, and no longer worth describing here

Chat and its error journeys, the chat-message extension point, MCP servers, prompt
libraries, and form validation for every resource that has a form. Each is a spec now,
and a spec describes itself better than a list of what it covers.

**Two of those had been listed as blocked on pages that already existed.** That is the
lesson worth keeping: a stale "blocked on" entry costs more than no entry, because it
stops somebody porting work that is already possible.

**What is still missing is the agent form, because there is no agent form.** An agent is
an `AgentTemplate` paired with a `Harness` and is not created. If a create-an-agent
surface lands, its validation belongs in that change.

## Was "not started by request", and is not that any more

App extension-point specs were held back while the framework's contract was unfrozen.
`extension-points.withExtension.spec.ts` and `extension-points-absent.spec.ts` both exist
and run, and between them assert every point the app declares — so this entry described
the tree accurately for about as long as it took the contract to settle, and then said
the opposite of the paragraph above it for longer. Anything still wanted here has to be
named; "the specs" is done.

## Lost with the REST path tests, and where it went instead

`src/api/readPaths.test.ts` and `src/api/writePaths.test.ts` are gone. They drove the API
client over REST URLs against the MSW fixture backend, and neither the URLs nor that
backend's REST routes exist any more — the controller serves its application API as
gRPC-Web. `src/api/operations.test.ts` replaces them, against the real generated service
descriptors served in-process, and covers strictly more of what those two were for: which
RPC each operation invokes, with what identity in the request message, and what the
response converts to.

**One property could not live there, and it now lives in a browser spec instead.** The old
write tests read each create *back through its list* — "the create returned 200" and "the
thing exists" are different claims, and only a stateful backend can check the second. The
in-process router is stateless per test, so `operations.test.ts` cannot. That property is now
`playwright/tests/harnesses/harnesses.spec.ts`: create a harness, land back on the tab it was
created from, and find it in the list — and find it reported "not ready yet", which is the
state a cluster reports for one the controller has not observed. Nothing about it is
deferred any more.

It lived in an agent-create spec until that page was removed: an agent is not something
anybody creates, so the form that appeared to create one went, and the read-back property
moved to the nearest thing that is genuinely created.

Two things worth keeping from writing it, because both cost time and neither is guessable:

- **Stay inside one browsing context.** The fixture backend keeps writes in the page's own
  memory, deliberately, so one spec's creates cannot leak into the next one's list. A
  `page.goto` therefore starts a backend that has never heard of the thing just created,
  and the failure reads as "the create did not stick" when nothing is wrong. Click through
  from the list.
- **The second read is the point.** `chat-capabilities-toggle` is asserted rather than the
  heading or the panel, because those render from the URL and would appear for an agent
  that does not exist. That button renders only when the per-agent read resolved a row, so
  it is what distinguishes "the list re-fetched" from "the thing exists". The weaker
  version of this spec passes and proves less than it looks like it does.

**Every resource reads its create back through its list now**, not only harnesses — see
*Covered: form validation, and every resource's lifecycle* above, which also records the create-cache defect
that used to make that impossible for three of them and how the fix was proved.

---

## Lost when agents became AgentInstances

One thing the suite used to cover no longer exists, and it should not be replaced by a
passing test of something adjacent. Two others that were listed here — the capabilities
panel and the sharing loop — have since come back and are covered.

### An agent's own tools, model and readiness on its details page

The details page showed a `SandboxAgent`'s spec: its model resolved from a `ModelConfig`,
its tool bindings, and its `Ready` condition with a reason. It now shows the
`AgentInstance` record instead — state, operation, the pair it was cut from, the prepared
revision, the A2A authority and the failure — which is the whole of what the API knows
about an instance.

That is not a reduction to fix: an instance genuinely has no spec. The configuration
belongs on the `AgentTemplate` and `Harness` surfaces, and those exist now — the agents
landing page carries all three as tabs, and a conversation's record links out to the
template and to the agent rather than duplicating either. What is still not covered in a
browser is that an agent's readiness *reason* is readable end to end, because the
`AgentInstance` record reports a failure message and the template reports a condition, and
no single surface shows both. Nor is either half: `AgentDetailsPage` — the record itself,
with `instance-state`, `instance-operation`, `instance-failure` and the sentence saying
what a state means — has no spec at all. A gap rather than a deferral, and worth taking
before the end-to-end one.

## What the chat fixes could not be covered against

Three gaps left by the work on the reader's own message, the artifact-append streaming
and the lifecycle indicator. Each is a *mock* gap: the mock backend cannot produce the
state the assertion would need, and inventing one would make the fixture the thing being
tested.

### The suspending stage of the lifecycle indicator

`chat.spec.ts` drives the indicator through its resting reading and through `running`,
because a turn produces both. It never sees `resuming` or `suspending`: those come from
`AgentInstance.operation`, which the controller claims and clears as it works, and the
mock backend serves a static record. Faking one would prove only that a fixture can hold
a string.

The reading itself is covered exhaustively in `src/components/chat/lifecycleReading.test.ts`
— including the case worth guarding hardest, that **no stage is claimed when a turn ends**,
since a substrate agent really does suspend itself then and nothing in the API reports it.
What is missing is a browser journey that suspends an instance from the agents list while a
chat page is open on it and watches the indicator follow. That belongs in `playwright/live/`,
where the operation is real — and that lane now exists and runs in CI, so the blocker has
moved rather than gone: an `AgentInstance` exists only once a message has been sent, and
neither cluster has a model that can answer one. CI installs with `OPENAI_API_KEY: fake`,
and `setup-cluster.sh` sets no key at all.

### Streaming, end to end, against a controller

The client now honours an artifact's `append` flag, which is how this runtime streams: one
`artifactId` for the reply, one frame per token, `append` on every frame after the first,
then a closing frame repeating the whole answer. That shape is pinned in
`src/api/chat/a2aGrpcChatClient.test.ts` against frames captured from the controller on
2026-08-24, and it was confirmed by hand — `grpcurl` at the gateway, and a throwaway
Playwright run against a live instance that rendered the reply.

**The mock chat client does not reproduce that shape.** It streams with `delta` events,
which is the port's own vocabulary rather than the wire's, so no browser test exercises the
artifact path. Teaching the fixture to emit artifact frames would mean it stopped being a
`ChatClient` and started being an A2A server, which is the wrong seam — the transport is
already covered by unit tests over real bytes. The browser-level gap is a `playwright/live/`
spec that sends a message and asserts the reply grows on screen before the turn completes.
The lane is no longer what blocks it: a reply that streams needs a model that answers, and
both clusters this suite runs against carry a fake key. Reachable today only by a developer
with their own.

A related gap worth naming rather than leaving implicit: the mock backend serves one
instance per conversation and never *changes* an instance's `operation`, so the lifecycle
indicator's `resuming` and `suspending` stages have no browser coverage either. Both
belong in the same live spec.

### Tool approval, and a question asked without the extension

`ask_user` is now answerable end to end: the question renders with its choices, the
answer names the parked turn and carries the extension payload, and the agent uses it.
What is left are the two neighbouring cases, both of which the UI *recognises* and says
plainly rather than guessing at.

**A `tool_approval_request`** is **no longer deferred.** The product decision landed in
#2714 and the controls shipped with it; they had no browser coverage until
`tests/chat/approvals.spec.ts`, which drives both shapes — several tools decided
independently behind a Submit, and a single tool decided on the prompt itself — and reads
the decisions back off the reply rather than off the form, so it fails if the page sends
both as approvals or pairs a reason with the wrong tool.

**A turn parked without the HITL extension activated** has no payload at all — the
question exists only as prose and carries no correlation id, so no answer can be routed
to it. The prompt says so and offers the discard. This build always activates the
extension, so it can only arise from a turn started by something else (a `kubectl`-driven
send, an older client). It is not worth engineering around; it is worth not lying about —
and the sentence that does the not-lying is covered in the same spec.

**The `ask_user` payload still renders as JSON in the transcript**, beside the answerable
prompt — the tool call and its result are structured data and are shown as such. That is
now duplication rather than a defect, and collapsing it needs a decision about whether a
tool call that has an interactive rendering should still show its raw form at all.

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
allocated actor count reported by Substrate. A column would need the
walk per page.

**A single-message read is defensible only while the message really holds everything.**
`GetSubstrateStatus` is the read that failed this way once: a cluster of 410,110 actors
produces a response gRPC refused to send, which is why the substrate page was split into
three reads in the first place. That endpoint has been removed from `SystemService`. For the three reads at the top of this table that do still answer with
everything, **the moment one starts paging — or starts truncating to survive — its
client-side search and sort must be labelled or removed in the same change**, because an
unlabelled filter over a page reports "no matches" about a row on page nine.

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

---

## Auto-titling costs a read per row, so the table still does not do it

A conversation is named by the reader, and an unnamed one can be titled from its first
message — `ListTasks{ContextID: instanceId}` returns the history. That is **free on the
chat page**, which has already read the transcript because it is rendering it.

The **rail** now pays for the rest, bounded at thirty: every row but the open one used to
read `Untitled · 50b46891`, which made the list very nearly unusable — the one row a
reader could identify was the one they were already looking at. Thirty reads for a rail
somebody is navigating by is a trade worth making; failures are per-row and silent,
because a title is a convenience over an id that already identifies the row.

The agent's conversation **table** still falls back to `Untitled · <short id>`, and that
is a decision rather than an omission: it is the surface that could hold hundreds of
rows, and one read per row to put a label on them is the cost the rail's budget of thirty
exists to bound. What narrows it is described two sections down — the read is paged and
this client follows every page, and the search and sort are the browser's.

Two ways it could stop being a trade-off, both server-side and neither invented here:

- **`AgentInstance` carries the first message**, denormalised the way `description` and
  `model_config_ref` already are on `AgentTemplate`. One extra string on a message the
  list returns anyway, and no extra call at all.
- **`ListAgentInstances` gains a field mask** for it, so callers that want it pay and
  callers that do not are unaffected.

Either would let a list show what the chat page already shows. Until then, what a list
renders for an unnamed conversation is pinned by `agents/agent-page.spec.ts` —
both that it is never a bare UUID, and that the derived title appears where the
transcript is in hand.

## An agent's conversation search is over what was fetched, and the page-following is why

`ListAgentInstances` narrows to one agent **on the server**: it takes `agent_template`
and `harness` and resolves them through the prepared revision. That is the narrowing
that matters, because it is the one the paging is applied after. What the request does
**not** carry is a search term or a sort field, so the agent page's search box and column
sorts run in the browser.

That is honest here for a reason worth stating, because it is the one read on the list
above that is paged at all: the client follows every page token before rendering anything
(`INSTANCE_PAGE_LIMIT` in `api/grpc/operations.ts`), so what is in the browser is every
conversation with that agent rather than the first fifty. The page used to say so under
the table; that note was removed as commentary a reader has no use for, which leaves this
file as the record.

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
fixtures can be in, and the default correctly does nothing against them.

The opposite half *is* covered: with several harnesses nothing is chosen for the reader,
and a template no harness admits says so ("creating one, and being told when nothing
will run it").

**How it was checked instead:** against the live cluster, which has one harness
(`kagent`) — the same shape the default exists for.

**What would close it:** a fixture scenario with a single harness. Worth doing when
something else needs one; a scenario knob added for one assertion is a second fixture
backend to keep honest.

## A broken create takes that resource's failure states with it

Each resource spec runs its empty, failure and retry states after the lifecycle, and a
journey is ordered — so a create that breaks aborts the three steps least likely to be
broken by the same change. `agent-templates` did exactly that during this port: step 10
failed and steps 11 to 14 never ran.

The README justifies the position by the fixture reset — reaching those states needs
`?mock=`, which is per-navigation and discards what the lifecycle created. That is a
reason they cannot sit in the *middle*; it does not choose an end, because the reload
starts a fresh backend whichever end they are at.

Moving them first is not the fix either: they would then run against a pristine backend,
which is not the state they are about, and the lifecycle would start from one a
navigation had just reset. What actually removes the coupling is a second `test` in the
same file — one recording for the lifecycle, one for the states, neither able to abort
the other. That costs `conventions.test.ts` its "one spec, one test" rule, so it is an
amendment to the convention rather than a reshuffle, and belongs in its own change.
