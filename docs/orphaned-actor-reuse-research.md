# Research: DB-backed reuse of orphaned substrate session actors

Status: research / design exploration (feeds kagent#2111 — "previous actor artifacts lost on config rollout").

## 1. Problem

On a substrate SandboxAgent, each chat session gets its own actor, forked from the **golden
snapshot** of the agent's current config-hashed `ActorTemplate`. When the agent's config rolls
over:

- the session's actor id changes (it folds in the new config hash),
- so the next message **creates a fresh actor from the new golden** (pristine filesystem), and
- the old actor — now orphaned under the superseded config — is **reaped** (deleted when SUSPENDED;
  see `reapOrphanedSessionActors`).

Net effect: the session's accumulated in-actor state (the SRT working-dir filesystem, any
in-memory state) is **lost** on every config rollout. We want to persist "relevant config/state in
the DB" so orphaned actors can be **reused** instead of discarded.

## 2. How it works today (grounded)

| Fact | Where |
|------|-------|
| Actor id is **purely derived**, never stored: `asr-<ns>-<name>-<configHash>-<sessionID>` (sha256 fallback for long names) | `substrate/agent_actor.go` `SandboxAgentSessionActorID` |
| Session→actor resolution recomputes the id each request from the **current** template's hash | `agent_actor.go` `sessionActorRef` → `ResolveCurrentActorTemplate` |
| Config hash inputs: rendered `AgentConfig`, agent card, model/RMS secret hashes, `srt-settings.json`, skills-init config. **Image digest is NOT hashed.** | `translator/agent/adk_api_translator.go` `computeConfigHash`, `manifest_builder.go` |
| Current template = highest `kagent.dev/desired-generation` that is Ready (blue-green) | `lifecycle_shared.go` `selectCurrentActorTemplate` |
| Old templates + goldens are **retained** after a rollout; deleted only on SandboxAgent delete | `agents_backend.go`, `lifecycle_delete.go` `CleanupSandboxAgentTemplate` |
| Reaper deletes the agent's SUSPENDED session actors under **superseded** templates, on new-actor creation | `agent_actor.go` `reapOrphanedSessionActors` |
| ate-api ops: `CreateActor(id, tmplNS, tmplName)`, `GetActor`, `ResumeActor`, `SuspendActor`, `DeleteActor`, `ListActors`. CreateActor binds to a template **by name**; resume restores the actor's own checkpoint | `substrate/client.go` |
| **DB has no actor/config state.** `session` table columns: `id, user_id, name, created_at, updated_at, deleted_at, agent_id, source` — no JSON blob, no actor id, no config hash | `api/database/models.go`, `migrations/core/000001_initial.up.sql` |
| Migrations: golang-migrate (`migrations/core/000NNN_*.up/down.sql`) + sqlc (`internal/database/queries/*.sql` → `gen/`). `session_share` (000006) is a clean precedent for adding a per-session side table | `references/database-migrations.md` |

Two consequences worth internalizing:

1. **There is no persistent record of which actors a session has ever had.** Reuse is impossible
   today not because the actors are gone (their goldens are retained) but because nothing remembers
   the `(session, configHash) → actorId` lineage, and `sessionActorRef` only ever computes the
   *current* hash's id.
2. **Goldens are immutable and config+fs+memory are baked together.** Substrate offers no "rebase
   this actor's filesystem onto a different golden" primitive. This is the crux (section 3).

## 3. The central constraint: what "reuse" can mean

A golden is an immutable image of `(config, initial fs, initial memory)`. A session actor forked
from golden-H accumulates state *on top of* H. So:

- **Reuse the OLD actor** → you get **old config + preserved state** (the old golden runs).
- **Create a NEW actor** → you get **new config + pristine state** (today's behavior).
- **New config + old state** → requires moving the accumulated filesystem/state from one golden to
  another. **Substrate cannot do this transparently** — it must be done at the application layer
  (export the relevant artifacts, replay them into the fresh actor).

So "reuse orphaned actors" splits into two genuinely different features:

- **(A) Resume-on-config-match** ("flip-back reuse"): if the config returns to a hash the session
  already has an actor for, resume that actor instead of creating a new one. Clean — the golden
  matches the config — and preserves that config's state. The DB registry alone enables this.
- **(B) Forward state-carry**: after a *forward* config change, seed the new actor with the old
  actor's artifacts. This is the harder, higher-value case and needs app-level state export/import;
  the DB holds the carried state, not just a pointer.

## 4. Design options

### Option 1 — DB actor registry (`session_actor` side table)

Persist the lineage the system currently throws away. Mirrors the `session_share` pattern (additive
side table, no change to `session`).

```
session_actor:
  id           BIGSERIAL PK
  session_id   TEXT NOT NULL
  user_id      TEXT NOT NULL
  agent_ns     TEXT NOT NULL
  agent_name   TEXT NOT NULL
  config_hash  TEXT NOT NULL
  actor_id     TEXT NOT NULL          -- the derived ate-api id
  template_name TEXT NOT NULL
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
  last_used_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
  -- optional: status cache, golden_actor_id
  UNIQUE (session_id, user_id, config_hash)
```

- **Written** by `EnsureSessionActor` on create/resume (upsert, bump `last_used_at`).
- **Read** by `sessionActorRef`/`EnsureSessionActor` to decide resume-vs-create, and by the reaper
  to know which actors are reuse-eligible.
- **Enables (A):** on a flip-back to config H, look up `(session, H)`; if its actor still exists,
  `ResumeActor` it (state preserved) instead of `CreateActor`.
- **Makes reaping reuse-aware:** instead of "delete all suspended orphans," keep the newest *K*
  config hashes per session and reap older / TTL-expired ones. ate-api remains source of truth; the
  registry is a cache that must self-heal (an actor missing in ate-api → drop the row; a row missing
  for a live actor → the reaper still catches it by template).

Pros: small, additive, leverages an existing precedent; directly delivers flip-back reuse and
smarter cleanup. Cons: registry/ate-api drift to reconcile; retention policy needed or actors
accumulate; **does not** solve forward state-carry on its own.

### Option 2 — Persist the rendered config per hash (`agent_config_revision`)

Literally "relevant config in the DB": store the rendered `AgentConfig`/agent-card/srt-settings (or
just a content-addressed blob) keyed by `config_hash`. Today that config lives only in a per-hash
in-cluster Secret. A DB copy enables: auditing what each hash *was*, diffing configs to decide
whether a change is "reuse-safe," and reconstructing context for reuse decisions. Complements
Option 1; modest cost.

### Option 3 — App-level state snapshot/restore (the real forward-carry fix)

To get **new config + old artifacts**, the runtime must export the session's workspace
(`get_session_path(...)` SRT working dir, and any other durable state) to external storage
(object store or a DB blob) — on suspend or periodically — and **seed a fresh actor** under the new
golden from that snapshot on its first message. The DB holds a pointer/metadata; bytes likely go to
the same object store as goldens (rustfs/GCS). This needs ADK runtime hooks (export/import) and is
substantially more work, but it's the only path that actually closes kagent#2111's "artifacts lost
on rollout."

## 5. Recommendation (phased)

1. **Phase 1 — registry + config-aware reaping (Options 1 + 2).** Lowest risk, additive schema,
   delivers flip-back reuse and turns the current "reap all suspended orphans" into "retain newest-K
   resumable, reap the rest / TTL." This is the natural home for "relevant config in the DB."
2. **Phase 2 — forward state-carry (Option 3).** The bigger lift; do it once Phase 1's registry and
   storage plumbing exist. This is what truly preserves a session's files across a *forward* config
   change.

Critically: **decide which problem we're solving first**, because Phase 1 alone does *not* preserve
artifacts across a forward rollout — it preserves them across flip-backs and prevents premature
deletion. If the actual pain is "user changed the model and lost their uploaded files," that's
Phase 2.

## 6. Interactions & risks

- **Reaper coordination:** reuse and the just-added sweep are in tension. The reaper must not delete
  registry-tracked, reuse-eligible actors; reuse must not resume an actor the reaper raced to delete.
  A single retention policy (newest-K per session + TTL, enforced in one place) avoids both deleting
  too eagerly and leaking forever.
- **Image digest not in the config hash:** resuming an old actor runs its golden's **old app image**
  (image upgrades don't change the hash). For flip-back this is usually fine, but it means "same
  config hash" ≠ "same binary." Document, and consider folding the image digest into the hash if
  binary freshness matters.
- **Capacity:** retained resumable actors are SUSPENDED → pin no workers, but more goldens/actors =
  more snapshot storage. Bound with retention.
- **Source of truth:** ate-api is authoritative; the registry is a cache. Handle drift (actor gone,
  row stale) and `ListActors` eventual consistency (already observed).
- **Lifecycle GC:** session soft-delete and SandboxAgent delete must clean registry rows (and, for
  Phase 2, the stored artifacts). `CleanupSandboxAgentTemplate` already iterates all templates.
- **DB rollout safety:** additive nullable/defaulted columns or new tables only (per migration rules).

## 7. Decision: Option A (resume-on-flip-back), DB-backed

Chosen direction. But researching A surfaced a sharpening that changes *what* to build:

### 7.1 Key insight — resume already works; this is a RETENTION problem

The resume mechanism for flip-back **already exists** and needs no new code:

- The actor id is a deterministic function of `(agent, configHash, sessionID)`. On a flip-back to
  config H, `sessionActorRef` derives the **same** id it used for H before.
- If that actor still exists, `EnsureSessionActor`'s `GetActor` **hits** → `ResumeActor` → the actor
  restores from its own checkpoint, **state preserved**. No registry needed for the resume itself.

The *only* reason flip-back reuse fails today is that the reaper **deletes** the H actor once config
moves off H (it's a SUSPENDED orphan under a superseded template). So Option A is really:

> **Make reaping reuse-aware: keep the actors a session is likely to flip back to; reap the rest.**

### 7.2 Two ways to decide "what to keep"

**(i) No DB — template-generation heuristic.** Rank the agent's ActorTemplates by
`kagent.dev/desired-generation` and retain session actors under the **newest-K** templates (not just
the single current one); reap actors under older templates. Purely derivable from objects already in
the cluster — zero schema change. Limitation: the "keep" set is **global to the agent**, not
per-session. A session that wants to flip back to a config that is old *globally* but was its own
most-recent gets no benefit.

**(ii) DB registry — per-session LRU/TTL.** A `session_actor` side table records, per
`(session, configHash)`, the actor id and `last_used_at`. Retention becomes **per-session**: keep
each session's newest-K (or TTL-bounded) configs resumable, reap the rest. This is what genuinely
needs the DB, and what the "maintain relevant config in the DB" ask points at.

> Build the DB registry only if **per-session** reuse policy (or its observability) is wanted. If
> "keep the last few configs for the whole agent" suffices, the heuristic (i) is far cheaper. This
> is the real decision before implementing.

### 7.3 Concrete Phase-1 design (DB registry, option ii)

**Schema** (`migrations/core/000007_session_actor.up.sql`, additive — mirrors `session_share`):

```
session_actor:
  id            BIGSERIAL PK
  session_id    TEXT NOT NULL
  user_id       TEXT NOT NULL
  agent_ns      TEXT NOT NULL
  agent_name    TEXT NOT NULL
  config_hash   TEXT NOT NULL
  actor_id      TEXT NOT NULL
  template_name TEXT NOT NULL
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
  last_used_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
  UNIQUE (session_id, user_id, config_hash)
```
Plus sqlc queries: `UpsertSessionActor`, `ListSessionActorsForSession`, `ListResumableForAgent`
(or similar), `DeleteSessionActor`. (`internal/database/queries/session_actors.sql` → `sqlc generate`.)

**Write points** (`EnsureSessionActor`): on create *and* on resume, upsert the row and bump
`last_used_at`. This is the actor backend's first DB dependency — it needs a `database.Client`
handle injected (today it has only the ate-api client + kube client).

**Read / retention points** (`reapOrphanedSessionActors`): instead of "reap every SUSPENDED orphan
under a superseded template," compute the **keep set** = each session's newest-K config hashes (from
the registry, by `last_used_at`), and reap only SUSPENDED orphans **not** in the keep set. RUNNING
ones still skipped; goldens still never touched. A TTL (e.g. reap registry-tracked actors unused for
N days) bounds growth.

**GC / consistency:**
- Session soft-delete (`SoftDeleteSession`) and `DeleteSandboxAgentSessionActor` must delete the
  session's registry rows (and their actors).
- SandboxAgent delete (`CleanupSandboxAgentTemplate` + `DeleteAllSandboxAgentActors`) must clear the
  agent's rows.
- ate-api is source of truth: a row whose actor is gone in ate-api is dropped lazily on next sweep;
  the registry is a cache, never authoritative.

**Reaper ↔ reuse tension:** retention is enforced in ONE place (the keep-set computation) so we
never both "keep for reuse" and "reap" the same actor. Resume races delete only within a session's
own serialized request flow, which is already the case.

## 7.4 Reframing (decided): support the invariant, don't manage orphans

Feedback on the above: retention (whether DB or heuristic) is **working around** a core invariant we
should support — **one session = one actor, with its filesystem preserved across config changes.**
Orphans only exist because the design *deliberately* mints a new actor per config. So the real task
is to make that invariant hold, not to curate the debris.

### Root cause (verified)

The agent loads its config **once at startup** (`MaterializeFromEnv` → `/config`, read once;
`go/adk/cmd/main.go:84`), and a golden is an **immutable memory snapshot taken after startup**. So
config is *welded into the golden*. Everything else follows mechanically:

```
config baked into immutable golden
   → a config change needs a NEW golden (substrate snapshots once, never re-snapshots)
   → new golden needs a NEW ActorTemplate (name carries the config hash)
   → to move sessions onto it, the actor id ALSO folds in the hash
   → so a config change yields a new actor id → re-fork from new golden → FS LOST
   → old actor is orphaned → reaper deletes it
```

The config-hash-in-actor-id, the orphan, and the reaper are all **symptoms** of welding config to
the golden. Remove that coupling and they all disappear — one stable actor per session, FS intact.

### The genuine fix: decouple config from the golden (runtime config)

`KAGENT_URL` already gives the runtime a live channel to the controller (used today for session
persistence). The root fix is to **stop baking config into the golden** and instead have the agent
**load/refresh its config from the controller at runtime**:

- One **config-agnostic golden per agent** (or per image), snapshotted in a state that loads config
  per-session/on-change rather than freezing it in memory.
- One **stable actor id per session** (drop the config hash from the id) → on a config change the
  **same** actor keeps running, re-applies the new config live, and **keeps its filesystem**.
- The config-hash template fan-out, orphan reaping, blue-green template pivot, and per-config Secrets
  **go away** for soft-config changes.

**Important boundary — soft config vs image-shape config.** Two classes of change:
- **Soft config** (model/provider, system message, tools, prompt, SRT network policy): just data the
  agent reads → hot-reloadable via the controller channel; no new golden. This is the common rollout
  and the whole pain point.
- **Image-shape config** (declarative runtime go↔python, `executeCodeBlocks`/skills → slim↔full
  image): changes the *binary/filesystem* of the image, which genuinely needs a new golden. These
  are rare and can still take the new-actor path (and legitimately reset state). Folding only these
  into a golden identity is correct; folding *soft* config in is the over-coupling to remove.

### Cost / risk of the root fix (be honest)

- **Runtime change:** the ADK must build the model client / prompt / tools from config fetched at
  runtime and re-init on a config-version change, rather than at import/startup. Must avoid freezing
  config in the snapshotted memory (or re-init deterministically on restore) — this is the crux and
  needs validation against substrate's snapshot/restore.
- **Snapshot point:** the golden must be captured in a "config-not-yet-bound" (or rebindable) state,
  which may change how/when the golden is taken.
- **Perf:** re-initializing config-dependent objects on change has a cost; for the steady state it
  should be a no-op (same config version).
- **Scope:** still need a golden per *image shape*; only soft-config is decoupled.

### Alternatives that only partially support the invariant

- **Externalize the FS** (per-session persistent volume / object-store-backed workspace): the actor
  can re-fork from any golden without losing files. But it preserves FS only (not in-memory state),
  and per-actor durable mounts in on-demand gVisor actors may not be supported by substrate.
- **State export/import on re-fork** (snapshot the workspace on suspend, replay on create): preserves
  FS across a re-fork but is still a re-fork — exactly the "work around the invariant" the feedback
  rejects.

### Recommendation

Pursue the **root fix (runtime config / config-golden decoupling)** for **soft config**, and keep a
new golden only for **image-shape** changes. This is the design that actually delivers "one session,
one actor, FS preserved," and it *removes* the config-hash/orphan/reaper machinery rather than
feeding it. The retention/registry options (§4, §7.3) become unnecessary for soft config and are not
worth building if we commit to the root fix.

Next step should be a **feasibility spike** on the one load-bearing assumption: can the agent runtime
defer/refresh config from the controller such that a single golden serves all soft-config revisions
under substrate's snapshot/restore? If yes, the rest is mechanical removal of the hash coupling.

## 8. Remaining open questions

1. **Per-session policy (DB) vs global newest-K (no DB)** — §7.2. If global is acceptable, we can
   skip the schema entirely. (You chose DB-backed, which implies per-session; confirm that's the
   intent vs. just wanting *some* reuse.)
2. Retention knobs: K (configs kept resumable per session) and/or TTL.
3. Should the app **image digest** be folded into the config hash, so a flip-back never silently
   resumes an actor running a stale binary? (Image digest is currently NOT in the hash.)
4. Capacity: retained resumable actors are SUSPENDED (pin no workers) but cost snapshot storage —
   what's the acceptable ceiling per agent/session?
