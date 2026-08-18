# API v2 execution plan

## Summary

Land API v2 through four milestones:

1. Merge #2362 and freeze final CRD/gRPC contracts.
2. Deliver a usable single-agent vertical slice with the existing kagent runtime.
3. Add composition, Codex, Claude, UI/CLI/MCP cutover, and remove legacy APIs.
4. Add checkpoint/fork after upstream Substrate snapshot support.

API v2 is not complete until checkpoint/fork and their Substrate dependencies pass E2E coverage.

Public invariants:

- `Harness` and `AgentTemplate` are `kagent.dev/v1alpha3` CRDs.
- `AgentInstance` is a PostgreSQL-backed gRPC resource.
- One AgentInstance owns one rooted template tree and one A2A context.
- `A2A context_id == AgentInstance.id`.
- A2A owns interaction and history; AgentInstanceService owns catalog, lifecycle, metadata, and sharing.
- Substrate is the only compute backend.
- No public scheduling, service-account, Deployment, channel, profile, or BYO fields.
- V1 release-blocking adapters are kagent, Codex, and Claude.

## PR dependency graph

```text
K0 #2362
 ├─ K1 CRDs ─ K3 Preparation ─┬─ K7 Plugins
 │                            ├─ K8 Shared tools ─ K9 Dedicated tools
 │                            └──────────────┐
 ├─ K2 gRPC contracts ───────────────┐     │
 └─ K4 Runtime A2A gRPC ─────────────┴─ K5 AgentInstance create/delete
                                           ├─ K6 suspend/resume
                                           └─ K10 public A2A gateway
                                                ├─ K11 delegation/approvals
                                                ├─ K12 UI
                                                └─ K13 CLI/MCP/content

K3 + K4 + K8 + K9 ─┬─ K14 Codex adapter
                     └─ K15 Claude adapter

S1 Substrate snapshots ─ S2 snapshot-sourced actors ─ K16 dependency adoption
K6 + K10 + K16 ─ K17 checkpoints ─ K18 fork

K12 + K13 + K14 + K15 + K18 ─ K19 legacy removal ─ K20 release conformance

S0 ate-api ActorTemplate/ActorTemplateVersion ────────────┐
K3 + K5 ─────────────────────────────────────┴─ K5A backing-resource cutover (when ready)
```

K7 and K8 can run in parallel after K3. The external Substrate track can run alongside all kagent work. K3 and K5 initially use Substrate's existing Kubernetes `ActorTemplate` API; adoption of the future ate-api `ActorTemplate`/`ActorTemplateVersion` resources is a later, isolated cutover.

## PRs

### K0 — Merge #2362: controller REST-to-gRPC transport ✅

Scope:

- Merge #2362 without AgentInstance or SessionService cleanup.
- Keep private `TaskStoreService` wrapping upstream A2A `Task`.
- Preserve existing user-facing A2A and session/task behavior.
- Pin generation inputs and ensure committed output is reproducible.

Gate:

- Go, Python, UI, Buf, Helm, and generated-output checks pass.
- This becomes the base for every subsequent PR.

### K1 — Add final v1alpha3 configuration CRDs ✅

Add `Harness` and `AgentTemplate`; do not mutate old CRDs into the new meanings.

`Harness`:

- Initially supports typed `kagent`, `codex`, and `claude` variants.
- Owns digest-pinned runtime image, environment/credential references, WorkerPool reference, snapshot policy, admission selector, capabilities, and health conditions.
- Creates no Actor merely by existing.
- Has no service account, Kubernetes scheduling, Deployment, channel, native-profile, or generic extension fields.

`AgentTemplate`:

- Owns model, prompt, prompt-template sources, MCP tools, standalone skills, plugin selections, and AgentTemplate-backed tools.
- Agent bindings have `name`, required routing `description`, same-namespace `templateRef`, and `Shared`/`Dedicated` isolation.
- Explicitly lists acceptable Harnesses.

Status:

- Harness status publishes one controller-derived capability record.
- AgentTemplate status has one bounded preparation entry per explicitly requested Harness: desired revision, latest successful revision, and `Accepted`, `ResolvedRefs`, `Compatible`, and `Prepared` conditions.

Also regenerate deepcopy code, CRDs, Helm CRDs, RBAC, and validation tests.

### K2 — Freeze lifecycle and checkpoint protobuf contracts

Add generated contracts but register services only as their implementations land.

Services:

- `AgentInstanceService`: create, get, list, suspend, resume, delete.
- Share RPCs: create, list, and revoke AgentInstance shares.
- `CheckpointService`: create/get/list/delete checkpoint and fork.
- Standard `google.longrunning.Operations` contract for checkpoint and fork.

Semantics:

- IDs are opaque and server-generated.
- Creation requires namespace, Harness, AgentTemplate, and caller-scoped `request_id`.
- Listing defaults to creator ownership; audited operators may request all creators.
- Labels are copied immutably from the root AgentTemplate.
- Share creation returns the secret token once; listing returns share IDs and metadata; revocation uses share ID.
- Read-only shares allow A2A get/list/subscribe; read-write shares additionally allow send/cancel.

Use a pinned upstream A2A v1 protobuf dependency. Do not maintain an editable kagent fork of upstream A2A definitions.

### K3 — Preparation and immutable revision storage

Implement the single-boundary compiler using existing SandboxAgent compilation code:

- Resolve bilateral Harness/AgentTemplate attachment.
- Render prompts and ConfigMap `include` sources.
- Extract reusable prompt, model, and MCP helpers instead of synthesizing a SandboxAgent or duplicating its compiler.
- Resolve Harness, ModelConfig, and MCP credential references for validation and hashing. Preserve Kubernetes credentials as `SecretKeyRef` entries in the generated workload; never persist or report their values.
- Initially reject standalone skills, plugin bundles, and AgentTemplate-backed tools with precise unsupported-field conditions.
- Create one immutable `ate.dev/v1alpha1` Kubernetes ActorTemplate per prepared revision, using a deterministic revision-hashed name. Do not emulate the future stable ActorTemplate/ActorTemplateVersion split.
- Build the Kubernetes ActorTemplate directly: pinned Harness image, environment/config values and `SecretKeyRef`s, worker-pool selector, `/data` DurableDir, snapshot policy, gRPC port 80, and HTTP readiness on `/readyz:8081`. Do not create a PodTemplate or Kubernetes config Secret.
- Use the fixed `gvisor` sandbox class supported by the Kubernetes API. Do not add a Harness field or controller flag; `gvisor-default` SandboxConfig selection starts with the later ate-api cutover.
- Watch Kubernetes ActorTemplate status for readiness and golden-snapshot completion.
- Record sanitized prepared revisions in PostgreSQL, including source identities and hashes, resolved egress destinations, Kubernetes ActorTemplate namespace/name/UID, phase, and golden-snapshot identity.
- Keep the last successful revision usable while a newer revision prepares.
- Retain revisions through direct database foreign keys from attachments and, later, instances and checkpoints. Do not add generic artifacts or reference counters.
- Retire attachments without blocking Harness or AgentTemplate deletion; delete unreferenced versions immediately and let the last instance/checkpoint release trigger deferred cleanup.

Compile resolved model and MCP destinations into the revision for K5 to materialize as actor-scoped egress policy. K3 does not create EgressPolicy or Credential resources.

No Actor is created in this PR.

### K4 — Add upstream A2A gRPC to the kagent runtime

Add the pinned upstream A2A v1 gRPC server to Go and Python kagent runtimes:

- Support send, streaming send, get/list/cancel/subscribe, and gRPC health.
- Continue using kagent's existing session and TaskStore services until the public gateway owns persistence; mount the K3 DurableDir without switching these stores to SQLite.
- Serve the private runtime contract on upstream A2A gRPC port 80; legacy HTTP/JSON-RPC compatibility is not required.
- Serve a health-only HTTP `/readyz` endpoint on fixed port 8081 for Substrate readiness. It is not an A2A or legacy compatibility endpoint.
- Keep #2362’s TaskStore integration for legacy SandboxAgents until K19.
- Do not change public controller routes yet.

This establishes the final gateway-to-runtime contract independently of AgentInstance.

### K5 — AgentInstance create/get/list/delete

Add PostgreSQL tables and the registered service implementation:

- `agent_instance`
- `agent_instance_member`
- creation idempotency records
- AgentInstance shares
- prepared-revision references and tombstones

Creation:

- Select the latest successful prepared revision.
- Reserve caller/namespace/request ID transactionally.
- Execute create and delete as imperative Substrate workflows within their RPCs.
- Create and resume the deterministic Substrate Actor through Substrate's imperative APIs; a successful resume returns it `RUNNING`, with ActorTemplate `/readyz` as the readiness contract.
- Publish the logical A2A authority and transition to `READY`.
- Retrying a canceled create with the same request ID resumes the same deterministic workflow.
- Never duplicate a member while creation outcome is unknown.

Deletion fences interaction, deletes owned Actors, releases its prepared-revision foreign key, triggers cleanup when that was the final reference, and leaves an indefinitely retained V1 tombstone. No retention configuration is added until scale requires one.

Start with single-member prepared revisions; K9 extends the same state machine to multiple members without changing the public API.

### K5A — Adopt ate-api ActorTemplate versions when available

After Substrate ships the stable ate-api ActorTemplate, immutable ActorTemplateVersion, exact-version Actor creation, and required credential handling:

- Replace K3's Kubernetes ActorTemplate creation and watches with one stable ate-api ActorTemplate per attachment and one immutable ActorTemplateVersion per prepared revision.
- Use the Kubernetes namespace as the Atespace, leave the ActorTemplate default version unset, and select the fixed `gvisor-default` SandboxConfig.
- Replace K5's Kubernetes template reference with the exact prepared ActorTemplateVersion reference.
- Keep the compiler boundary, prepared-revision semantics and retention rules, latest-successful selection, and public APIs unchanged; replace only the stored backing-resource identity and its provisioning path.
- Require existing AgentInstances to be recreated during the cutover. Do not add dual-write, backfill, or a legacy compatibility path.
- Delete the Kubernetes ActorTemplate bridge in the same cutover.

This step is intentionally not on the critical path for the initial vertical slice.

### K6 — Suspend, resume, and failure reconciliation

Implement lifecycle across the complete member set:

- Suspend rejects active public or private work, fences ingress, and suspends every Actor.
- Resume restores every Actor, private bindings, and health before returning `READY`.
- Conflicting lifecycle operations return `ABORTED`.
- Missing or mismatched Actors transition the instance to `FAILED`; reconciliation never silently replaces them.
- Public history remains readable while suspended or failed.
- Emit lifecycle audit records, traces, and member-level failure details without exposing Actor IDs through ordinary APIs.

### K7 — Agent Plugins skills-only ingestion

Implement the selected Agent Plugins 1.0.0 subset:

- Immutable OCI digest, full Git commit, and versioned S3 ZIP sources.
- Validate the canonical root `plugin.json`.
- Materialize only explicitly selected Agent Skills and supporting files.
- Reject path traversal, escaping symlinks, duplicate skill names, mutable references, and oversized packages.
- Ignore `mcp.json`, client extensions, hooks, commands, apps, and other non-skill content.
- Pin contents and digest in the prepared revision.

Reuse existing artifact and skill materialization code where possible.

### K8 — Shared AgentTemplate tools

Extend preparation and the kagent adapter:

- Resolve the complete same-namespace rooted tree.
- Reject cycles, shared DAG nodes, different Harnesses, and consecutive Shared depth beyond one per runtime boundary.
- Compile Shared children into native kagent ADK subagents in the parent Actor.
- Preserve each parent-specific binding name, description, prompt, model, skills, and exact tool allowlist.
- Do not expose a public AgentInstance, Actor, or A2A Task for Shared children.

### K9 — Dedicated tools and multi-member topology

Add Dedicated boundaries:

- Produce a prepared runtime bundle and ActorTemplate per Dedicated boundary.
- Provision every member through K5’s existing state machine.
- Generate a binding-scoped private MCP server exposing only `invoke(task)`.
- Hide Actor identity, credentials, and arbitrary target selection.
- Persist private binding and conversation handles inside snapshot-covered storage.
- Apply lifecycle and deletion atomically to the complete member set.
- Keep child interaction terminal and tool-shaped; child streaming Tasks and child `INPUT_REQUIRED` remain deferred.

### K10 — Public AgentInstance A2A gateway

Make the controller the authoritative public A2A v1 gRPC server:

- Route by authenticated AgentInstance authority; never accept Actor addresses.
- Persist public Tasks, messages, artifacts, and an ordered append-only event log.
- Commit every update before publishing it.
- Enforce one non-terminal Task per AgentInstance transactionally.
- Implement message idempotency, conflicting-ID rejection, cancellation, reconnect, get/list, filtering, and history/artifact shaping.
- Perform Task filtering, stable ordering, total counting, and pagination in PostgreSQL with queries scoped by AgentInstance; Go validates page tokens and shapes history/artifacts but does not load, filter, or sort complete Task sets in memory or inherit session-backed query semantics.
- Generate the Agent Card from the pinned root template and gateway capabilities.
- Proxy to the root runtime’s private A2A endpoint without leaking private IDs.
- Never automatically replay an input after an ambiguous runtime disconnect.
- Apply creator/operator/share authorization independently for lifecycle, interaction, history, and cancellation.

The old session-backed A2A handler remains only until UI cutover.

K10 progress:

- Completed: authenticated routing and runtime proxying; durable Task and event persistence; database-backed get/list/filter/count/pagination and history/artifact shaping; the one-active-Task constraint; and message idempotency with conflicting-ID rejection.
- Remaining parallel slices: cancellation and reconnect/subscription; generated Agent Card; creator/operator/share authorization coverage; and Kind end-to-end coverage.

### K11 — Agent-to-Agent delegation and approval profile

Add the remaining interaction semantics:

- Target-ready and busy checks.
- Short-lived target-scoped delegated credentials.
- Trusted parent/root Task lineage, depth limits, deadline propagation, and cycle detection.
- Optional MCP convenience invocation routed through the same gateway.
- Versioned A2A `DataPart` profile for approvals and input-required continuation.
- Reject untrusted lineage or authorization metadata supplied by runtimes.

Each accepted delegated invocation creates a normal public Task on the target AgentInstance.

### K12 — UI and browser BFF cutover

Replace session-centric UI behavior:

- List AgentTemplates and AgentInstances separately.
- Create an AgentInstance before opening chat.
- Use AgentInstance ID as the conversation and routing identity.
- Replace session sidebars, rename, deletion, lifecycle status, and actor controls with AgentInstance operations.
- Use a thin Next.js BFF to bridge authenticated browser HTTPS/SSE to canonical A2A gRPC.
- Keep no Task state machine in the BFF.
- Move share-link UI to AgentInstance share RPCs.
- Render complete history using A2A `ListTasks`.
- Support suspended, resuming, failed, busy, reconnecting, and input-required states.
- Remove SandboxAgent and AgentHarness forms and ACP-specific chat branches.

UI work can begin against K2-generated mocks and merge after K10.

### K13 — CLI, MCP, examples, and Helm cutover

CLI:

- Apply Harness and AgentTemplate manifests.
- Create/list/get/suspend/resume/delete AgentInstances through gRPC.
- Invoke and follow Tasks through upstream A2A.
- Remove SandboxAgent, AgentHarness, Deployment, BYO, session, and ACP branches.

MCP:

- Discover ready AgentInstances.
- Invoke them only through the public gateway A2A path.
- Never expose Actor or private MCP endpoints.

Content:

- Convert examples to Harness/AgentTemplate/AgentInstance workflows.
- Ship no default Helm agents.
- Document that Substrate is mandatory.
- Update Helm RBAC and values for the new CRDs and gateway.
- Do not promise legacy session or CRD migration.

### K14 — Codex Harness adapter

Implement the second release-blocking adapter:

- Render Shared children into private `CODEX_HOME/agents/*.toml`.
- Render explicit MCP and Dedicated bindings into Codex configuration.
- Materialize selected skills into the pinned runtime layout.
- Drive `codex exec --json` and `codex exec resume`.
- Preserve threads, workspace, MCP handles, and adapter state in DurableDir.
- Map Codex output, tool calls, approvals, cancellation, and failures to the private upstream A2A service.
- Publish only capabilities proven by the conformance suite.

OpenClaw, Hermes, hosted profiles, and shared-host topology are separate future PRs.

### K15 — Claude Harness adapter

Implement the third release-blocking adapter:

- Render Shared children, explicit MCP bindings, and Dedicated bindings into Claude's native configuration.
- Materialize selected skills into the pinned runtime layout.
- Drive the pinned Claude runtime through its non-interactive streaming interface.
- Preserve conversations, workspace, MCP handles, and adapter state in DurableDir.
- Map Claude output, tool calls, approvals, cancellation, and failures to the private upstream A2A service.
- Publish only capabilities proven by the conformance suite.

### S1 — Upstream Substrate immutable ActorSnapshot API

External Substrate PR:

- Add immutable ActorSnapshot identity and Get/List/Delete APIs.
- Make durable suspend return the exact snapshot created.
- Add expected Actor UID/version fencing.
- Define FULL and DATA scope semantics.
- Add reference tracking and deletion protection.
- Test supported sandbox/storage implementations.

### S2 — Upstream snapshot-sourced Actor creation

Stacked external Substrate PR:

- Allow `CreateActor` from an immutable source snapshot.
- Create the target initially suspended with a new Actor identity.
- Validate ActorTemplate and snapshot compatibility.
- Ensure FULL restoration includes process and filesystem state.
- Preserve source snapshot and Actor immutability on partial failure.
- Add clone, resume, deletion, and incompatibility conformance tests.

### K16 — Adopt released Substrate snapshot APIs

- Replace the temporary kagent Substrate fork/version.
- Add client wrappers for fenced suspend, snapshots, and source-snapshot creation.
- Record exact snapshot identities for every runtime member.
- Verify kagent does not infer snapshot identity from “latest” state.
- Add integration tests for partial multi-member failure and reference cleanup.

### K17 — Checkpoint service

Implement persisted long-running operations:

- Fence the AgentInstance and reject active public, Shared, or Dedicated work.
- Capture the committed public history head and sequence.
- Suspend every member with UID/version preconditions.
- Publish a Checkpoint only after every member snapshot and history boundary commit.
- Restore the source instance to its prior running/suspended state.
- Keep partial checkpoints invisible and mark ambiguous failures explicitly.
- Delete only when no fork or retained lineage references its snapshots.
- Make repeated request IDs idempotent.

Checkpoint contents exclude external MCP-owned mutable state.

### K18 — Fork from checkpoint

Implement `ForkAgentInstance`:

- Require same-namespace target ownership.
- Create new Actor identities from every checkpoint snapshot.
- Rewire Dedicated bindings to the cloned members.
- Create a new AgentInstance, A2A authority, creator ownership, and labels.
- Keep source instance, history, and snapshots immutable.
- Represent inherited history through copy-on-write projections: deterministic fork-local Task IDs and the new context ID reference immutable source payloads and lineage without duplicating content.
- New Tasks append only to the fork.
- Return ready only after all members resume and pass health.

In-place restore is not added.

### K19 — Remove legacy APIs and storage

After K12–K18 are merged:

- Delete SandboxAgent and AgentHarness CRDs, controllers, translators, routes, RBAC, UI, CLI, ACP gateway, and generated artifacts.
- Delete `SessionService`, legacy session sharing, `TaskStoreService`, and runtime controller clients.
- Remove session/event/generic-agent tables and session TTL configuration.
- Retain the new AgentInstance, public A2A task/event, checkpoint, operation, and share tables.
- Retain only the browser BFF and canonical gRPC gateway.
- Remove obsolete tests instead of translating Agent-specific fixtures.
- Add repository checks proving no legacy kinds, routes, services, or Deployment-mode selectors remain.

No automatic migration of legacy Sessions or live SandboxAgents is provided. Alpha users must recreate configuration and instances.

### K20 — Release conformance and CI gate

Enable blocking clean-install coverage:

- kagent, Codex, and Claude Harnesses.
- Prompt/model/MCP/skills/plugins.
- Shared and Dedicated tools.
- Create idempotency and controller restart at each provisioning step.
- Public A2A send/stream/get/list/cancel/reconnect and busy rejection.
- Read-only/read-write shares.
- Suspend/resume with durable private state.
- Multi-member checkpoint consistency and fork independence.
- Snapshot failure, missing Actor, source deletion, and cleanup behavior.
- Browser BFF and UI workflows.
- CRD generation, Helm lint/render, Buf lint/breaking/generation, Go/Python/UI tests.

Substrate Kind E2E may remain disabled during earlier PRs, but it is blocking here. Legacy upgrade tests remain intentionally unsupported; the release gate tests clean installation and new-version persistence only.

## Parallelization

After K0:

- API lane: K1 → K3 → K7/K8 → K9.
- Control-plane lane: K2, then prepare K5 database/service work.
- Runtime lane: K4 independently.
- External lane: S0 can unlock K5A at any time after K5; S1 → S2 independently.
- UI lane: begin K12 against K2 mocks; integrate after K10.
- Codex and Claude lanes: begin after K3’s resolved-bundle contract; integrate independently after K8/K9.

High-conflict integration files should have one owner at a time:

- Protobuf/Buf configuration: K2, then K10/K17.
- Database migrations/sqlc: K3, then K5/K10/K17.
- Controller application wiring: K5, K10, then K19.
- Generated CRDs/RBAC: K1, then K19.

K7 and K8 should branch from K3 and avoid editing each other’s source-specific compiler packages. K14 and K15 should consume the resolved-bundle interface without modifying the central graph resolver.

## Milestone gates

- Preview 1: K0–K6 — single kagent AgentTemplate can prepare, instantiate, chat, suspend, resume, and delete through final APIs.
- Preview 2: K7–K15 — full configuration, composition, Codex, Claude, UI, CLI, and MCP behavior.
- Release candidate: S1–S2 and K16–K19 — checkpoint/fork complete and legacy surface deleted.
- API v2 complete: K20 passes with all three release-blocking adapters and Substrate E2E.

## Deliberate exclusions

- No AgentHost, HostedAgent, shared Actors, managed native profiles, or channels.
- No OpenClaw or Hermes release requirement.
- No BYO/fallback runtime.
- No cross-namespace references.
- No multiple conversations or parallel Tasks per AgentInstance.
- No template inheritance, BaseContext, shared-store, or filesystem CRDs.
- No external MCP state checkpointing.
- No legacy Session/SandboxAgent data migration.
- No placeholder APIs for deferred features.
