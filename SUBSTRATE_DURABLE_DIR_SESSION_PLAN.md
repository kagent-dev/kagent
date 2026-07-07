# Substrate durable-dir session state: persist SandboxAgent ADK sessions in-actor instead of postgres

**Status:** Design / plan (pre-implementation)
**Author:** (design captured 2026-07-02; revised against upstream substrate `main` after the
secret-volume work was confirmed **unplanned** — see §4.2; revised 2026-07-06 with the decided
direction: durable-dir sessions are the **default** for substrate SandboxAgents via a
controller-set env var (§4.4), and session events stay reachable through the controller API via
`GET /api/sessions/{id}/events?source=sandbox` (§4.5))
**Related:** `SUBSTRATE_PYTHON_BYO_PLAN.md`, `docs/orphaned-actor-reuse-research.md`,
`docs/substrate-python-byo-manual-validation.md`. (`SUBSTRATE_CONFIG_VOLUME_PLAN.md` is
**unplanned** and no longer a dependency of this plan.)
**Substrate feature:** commit `37b5006d92cc7c9318045b57ddd6d50468f4f5ec` "DurableDir support (#295)"
(+ `125180e` readiness probes #330). Both verified present in upstream `main` (`53f9848`).

---

## 1. Goal

Store a substrate SandboxAgent's **ADK session state** (session rows, event log, state dict — the
data the runner replays to rebuild LLM context each turn) in a **durableDir volume inside the
session's actor**, instead of round-tripping every event over HTTP to the kagent controller's
database (postgres on kind-kagent).

Requirements:

1. Session state survives actor **suspend/resume** (kagent suspends the session actor after every
   response — `go/core/internal/a2a/substrate_sandbox_transport.go:88`).
2. Session state survives **infrastructure restarts** (controller, atelet, ate-api, worker pods).
3. Session state survives **config rollouts** of the agent (change system prompt/model → same
   session keeps its history and picks up the new config).
4. The UI keeps working **without changing how it accesses session state**: session list, chat
   history reload, token stats, HITL approvals (all served by stores that don't move — §2), and
   session *events* stay fetchable through the controller API via a new read-through endpoint
   (§4.5) — the caller accepts that this resumes the actor, and the actor is suspended again
   afterwards.
5. This is the **default** for substrate SandboxAgents (no per-agent opt-in in the end state),
   selected at runtime by a controller-injected env var so the runtime keeps a separate,
   orthogonal code path from the existing HTTP session service (§4.4).

Non-goals: the Deployment (non-substrate) agent path; the Go declarative runtime and BYO images
(they get the mount + env-var contract but no runtime change yet — §5.7); ACP/agent-harness
actors.

---

## 2. Verified: where session state lives today

Double-checked in code **and live on kind-kagent** (2026-07-02):

- The python runtime inside the actor uses `KAgentSessionService`
  (`python/packages/kagent-adk/src/kagent/adk/_session_service.py`), an **HTTP client** —
  `POST /api/sessions`, `GET /api/sessions/{id}`, `POST /api/sessions/{id}/events` — pointed at
  `KAGENT_URL` (`http://kagent-controller.kagent:8083`). Wired in `KAgentApp.build`
  (`_a2a.py:94-101`) whenever not `--local`.
- The controller stores those in its database: `HandleCreateSession` →
  `DatabaseService.StoreSession`, `StoreEvents` (`go/core/internal/httpserver/handlers/sessions.go`,
  `go/core/internal/database/client_postgres.go`). On kind-kagent the DB is the
  `kagent-postgresql` pod.
- **Live proof** — events for substrate agents are in postgres right now:

  ```
  kagent=# SELECT s.agent_id, count(e.id) FROM session s JOIN event e ON e.session_id = s.id GROUP BY 1;
   kagent__NS__cfgvol_test         |      4
   kagent__NS__py_decl_substrate   |    486
   kagent__NS__test_decl_substrate |     72
  ```

**What does NOT move** (stays HTTP → postgres):

| Data | Store | Why it stays |
|---|---|---|
| A2A **tasks** | `KAgentTaskStore` → `/api/tasks` (`_a2a.py:149`) | UI chat history renders **from tasks**, not events (`ui/src/components/chat/ChatInterface.tsx:179` `getSessionTasks` → `extractMessagesFromTasks`); HITL resumability needs the task readable at the proxy |
| **Session rows** (metadata) | `POST /api/sessions` from the UI | session list/sidebar, share links, `read_only` flag |
| Memory service, feedback, push notifications | HTTP | unrelated to session-event storage |

The only server-side reader of session *events* is `HandleGetSession`
(`handlers/sessions.go:262`), and the UI only calls it on the share-token path for the
`read_only` flag (`ChatInterface.tsx:163`) — it never renders events. So moving events
in-actor does not break chat rendering. The `event` table itself is a thin envelope
(`id, user_id, session_id, timestamps, data TEXT`) whose `data` blob the controller never
parses — its only functional producer *and* consumer is the runtime's own session service, which
moves with the store. Clients that do want events for a sandbox session use the read-through
endpoint (§4.5). (The dead event helpers — `ListEventsByContextID`, `SoftDeleteEvent`,
`Event.Parse`/`ParseMessages` — have zero application callers today; anything new that reads
events from the DB must be audited before GA; see §6.)

---

## 3. What substrate's durable-dir gives us (and what it doesn't)

From commit `37b5006`, re-verified against upstream substrate `main` (`53f9848`). Note:
`VolumeSource` upstream supports **only** `durableDir` (`ExactlyOneOf={durableDir}`) — there is
no secret volume source; that was unplanned branch work. Config-as-env via
`valueFrom.secretKeyRef` **is** upstream (`actortemplate_types.go:195`), and ate-api resolves it
from the **live** Secret on every `ResumeActor` (`workload_spec.go` envResolver;
`TestResumeActorResolvesValueFromEnv`, `functional_test.go:1130`) — this is load-bearing for §4.2.

**API** (`pkg/api/v1alpha1/actortemplate_types.go`):

```yaml
spec:
  volumes:
    - name: data
      durableDir: {}            # DurableDirVolumeSource — no fields
  containers:
    - name: kagent
      volumeMounts:
        - name: data
          mountPath: /data      # clean absolute path, no trailing /
      readyz:                   # PR #330 — gates Run/Restore success on HTTP 200
        httpGet: { path: /health, port: 80 }
  snapshotsConfig:
    location: gs://ate-snapshots/kagent/<agent>
    onPause: Full               # scope for Pause (node-local snapshot)
    onCommit: Data              # scope for Suspend (uploaded to object storage) — must be ⊆ onPause
```

**Semantics:**

- The durable dir is a per-actor host directory
  (`/var/lib/ateom-gvisor/actors/{ns}:{templateName}:{actorID}/durable-dir/{vol}/`)
  bind-mounted into the container. It is **not** continuously replicated — it is captured at
  snapshot time and restored on resume.
- `SnapshotScope`:
  - **`Full`** = process memory + entire rootfs delta (durable dirs included). Resume is a warm
    memory restore. This is what kagent uses today (`onCommit: Full`).
  - **`Data`** = **only** durable-dir contents (`runsc fscheckpoint`). Resume **cold-boots** the
    container from the OCI image with the durable dir restored — the workload spec (including
    `secretKeyRef` env, re-resolved from the live Secret) is rebuilt from the current
    ActorTemplate at resume time; the `readyz` probe holds the Resume RPC until the app serves
    200 (without it, the first request after a Data-resume races bootstrap — the exact bug
    PR #330 fixed).
- Constraints: at most **one** durableDir volume per ActorTemplate; not supported on
  `sandboxClass: microvm`; suspend snapshots are keyed `{location}/{actorID}/…`.
- **What it does not give us:** there is **no cross-actor restore** — no API to seed a new
  actor's durable dir from another actor's snapshot. Durable dir data lives and dies with its
  actor (and its template-name-scoped host path).

**Version status:** durable-dir is **not** in any substrate tag. kagent pins
`github.com/kagent-dev/substrate v0.0.7` (`go/go.mod:453`), which predates it — verified: zero
`Volumes`/`Readyz` hits in the v0.0.7 module. Bump target: a pseudo-version at or past upstream
`main` `53f9848` (durable-dir + readyz, no unplanned extras). **Cluster caveat:** the kind-kagent
substrate (`localhost:5001/*:rsv2`) was built from a dev branch that *includes* the unplanned
secret-volume code (its CRD shows `volumes[].secret`). Before running the §7 runbook, rebuild and
redeploy substrate images + CRDs from upstream `main` so we validate against planned behavior
only.

---

## 4. Design

### 4.1 The session store: sqlite in the durable dir

One session ⇔ one actor (`SandboxAgentSessionActorID`, `agent_actor.go`), so the actor is the
natural home for that session's state. Reuse what already exists (no new session service):

- google-adk (pinned `>=1.28.1`) ships `DatabaseSessionService` (SQLAlchemy, already in
  `uv.lock` as a google-adk dependency). Point it at `sqlite:////data/sessions.db`.
- The substrate backend injects `KAGENT_SESSION_DB_URL=sqlite:////data/sessions.db` and mounts a
  `durableDir` volume at `/data`. `KAgentApp.build` picks `DatabaseSessionService` when the env
  var is set; everything else (task store, token service, memory) keeps the HTTP client.
- Per-turn durability is inherited from the existing lifecycle: kagent **suspends the actor after
  every response**, and suspend captures the durable dir. So the persistence cadence matches
  today's per-event HTTP writes at turn granularity.
- **Schema-migration invariant (load-bearing, keep true):** the sqlite file is only ever opened
  by the exact ADK version that created it. The actor's code is pinned by the digest-pinned
  image in the immutable ActorTemplate; soft rollouts don't change the image, and an image
  change is a shape change → new actor → fresh DB created by the new code at its latest schema.
  So **no in-actor schema migration path exists by construction** — long-running sessions never
  meet newer code. Any future feature that breaks this (cross-shape state carry, §5.7) must
  transport state at the **event-JSON wire level** (export via the §5.1 local-events endpoint,
  import through the new runtime's own service), never by copying raw sqlite files across image
  versions: google-adk versions its DB schema (`adk_internal_metadata.schema_version`, v0-pickle
  → v1-JSON already happened) and ships only **offline** migration runners
  (`google/adk/sessions/migration/`) — unusable inside a distroless actor.

### 4.2 The crux: rollout survival requires stable actor identity — solved with substrate as-is

Durable-dir data is keyed by `(namespace, templateName, actorID)` and there is no cross-actor
restore. Today a config rollout mints a **new** actor (config hash is folded into template name
*and* actor id) and reaps the old one — so a durable dir under the old actor is unreachable from
the new one. **Shipping durable-dir sessions under today's identity scheme would *regress*
rollouts**: with postgres, the post-rollout actor refetches full history over HTTP; with an
in-actor sqlite it would start empty.

The config-volume plan (secret volumes) is **unplanned**, so identity must be stabilized with
what upstream substrate `main` already provides. Two verified substrate facts make it work:

1. `ActorTemplate.spec` is **immutable** (`self == oldSelf` CEL, `actortemplate_types.go:353`) —
   templates can never be updated in place, so a stable template requires that *nothing
   config-derived appears in the spec*.
2. `secretKeyRef` env is re-resolved **at every resume** (`TestResumeActorResolvesValueFromEnv`),
   and a **Data-scope resume is a cold boot** — the process re-execs with the freshly resolved
   env. So Secret *contents* are the one config channel that changes without touching the
   immutable spec — and kagent already delivers config exactly this way
   (`KAGENT_CONFIG_JSON`/`KAGENT_AGENT_CARD_JSON`/`KAGENT_SRT_SETTINGS_JSON` via
   `kagentAgentSecretEnv` → `secretKeyRef`, preserved through `sanitizeActorTemplateEnvVar`,
   `lifecycle_shared.go:286`).

   **Two caveats, verified in code:**
   - *"Live" is modulo a 30s cache.* ate-api resolves env through a secret-value cache keyed by
     `(namespace, name)` with `envSecretCacheTTL = 30s` (`workload_spec.go:33`). A resume within
     ~30s of a Secret update may still boot the **old** config; it self-heals on the next
     suspend/resume cycle. Acceptable eventual consistency for rollouts — but the rollout test
     (§7.7) must tolerate/verify this window.
   - *The per-hash Secret name is not incidental — it is a deliberate cache-buster.* The comment
     at `agent_lifecycle.go` (`buildSandboxAgentActorTemplate`) explains: goldens materialize
     config at golden-build time, and with a **shared** Secret name the 30s cache could hand a
     *stale* config revision to a *new* golden, freezing the wrong config forever. Moving to a
     stable Secret name is therefore only safe **together with** `onCommit: Data` (M2 before or
     with M3): under Data scope the golden's frozen memory never serves a resume — every resume
     re-resolves env — so golden staleness degrades to the ≤30s visibility delay above. A stable
     Secret name under `Full` scope would resurrect the stale-golden bug that comment guards
     against.

**The scheme** (all kagent-side, zero substrate changes):

- **Stable per-agent config Secret**: stop per-hash cloning; one Secret name per agent, contents
  updated in place on config change.
- **Stable ActorTemplate per agent**: drop the config hash from the template name. The spec then
  contains only *shape*: image digest, command, literal env, mounts, readyz, snapshotsConfig.
- **Actor id keyed by template shape, not config**: replace the config hash in
  `SandboxAgentSessionActorID` with a hash of the rendered template spec (a "shape hash").
  Soft config changes don't touch the spec → same shape hash → **same actor**.
- **`onCommit: Data`** is *mandatory*, not an optimization: with no secret volumes upstream and
  Full-resume restoring old process memory, the Data cold boot is the **only** mechanism by
  which a resumed actor observes new Secret contents.

Rollout then falls out mechanically: soft config change = Secret update in place; the actor is
already SUSPENDED between turns; the next message's resume cold-boots with the new config env
and the restored `/data` — new behavior, old session state, same actor.

**The honest boundary — shape changes still reset session state.** An image bump, command
change, or a user editing a *literal* `spec.env` value changes the shape hash → new template +
new actor → fresh durable dir (spec immutability makes this unavoidable). Same state-loss class
as today, rare, documented. The existing orphan reaper stays for exactly these rollovers. (A
future substrate "seed durable dir from snapshot X" primitive is the only thing that would close
this; worth filing, not worth blocking on.) Corollary: model **API keys** should reach the actor
as `secretKeyRef` env — never literals — so key rotation is a soft change.

### 4.3 Snapshot scope: flip `onCommit` Full → Data (phased)

| | `onCommit: Full` (today) | `onCommit: Data` (target) |
|---|---|---|
| Suspend cost/turn | full memory + rootfs delta upload (100s of MB) | durable dir only (KBs) |
| Resume | warm memory restore | cold boot + readyz wait (python startup) |
| Config freshness on resume | ❌ old config frozen in restored memory — no refresh channel exists upstream | ✅ env re-resolved from live Secret at cold boot |
| Durable dir restored | ✅ (part of Full) | ✅ |
| New-session actor create | fork from Full golden (warm) | golden is Data-scoped → cold start (see §6) |

Phase the flip: land the volume + sqlite store with scopes **unchanged** (`Full`/`Full` — the
durable dir is captured inside Full snapshots, so behavior is identical and latency is
unchanged), then flip `onCommit: Data` and measure resume latency in the runbook (§7.10). Data
is the end state — it is what makes per-turn suspends cheap and it is the **only** config-refresh
mechanism (§4.2), so requirement 3 is unmet until the flip. Keep `onPause: Full` (kagent never
pauses today; it satisfies the `onCommit ⊆ onPause` rule and leaves a warm-pause tier available
as a future latency optimization).

### 4.4 Default-on, selected by a controller-injected env var

The runtime keeps **two orthogonal session code paths** behind ADK's `BaseSessionService`
interface, selected once at startup by the presence of an env var the controller sets during
translation:

- `KAGENT_SESSION_DB_URL` **set** (e.g. `sqlite:////data/sessions.db`) → local store in the
  durable dir. The value is the config, the presence is the switch, and removal is the rollback.
- **Unset** → `KAgentSessionService` (HTTP), byte-for-byte today's behavior. The Deployment path
  never sets it, so non-sandbox agents are untouched by construction.

This degrades gracefully under version skew in both directions: an old runtime image ignores the
env var and keeps HTTP sessions (works, just not durable-dir yet); a new image without the env
var stays on HTTP. The translator knows the declarative runtime, so the default is scoped
per-runtime: **python first**; Go when its local store lands (§5.7) — until then Go sandbox
agents simply stay on HTTP.

**Staging to default:** during M0–M2 validation the controller gates the env var behind the
`kagent.dev/session-store: durable-dir` annotation; at M3 (stable identity) it sets it
unconditionally for substrate SandboxAgents and the annotation flips to an **opt-out** escape
hatch (`kagent.dev/session-store: http`). Note the escape hatch must be an annotation, not user
env: kagent-set env wins over user `spec.env` (`actorTemplateEnvFromPodEnv` keeps the first
occurrence), so users cannot override the var from the CR.

### 4.5 Session-events read path: `GET /api/sessions/{id}/events?source=sandbox`

Decided design for keeping events reachable through the controller API (the caller accepts that
this resumes the actor; the actor must be suspended again afterwards):

- **New controller route** `GET /api/sessions/{id}/events` — free namespace today (only POST is
  registered on that path, `server.go:249`), and it completes the REST symmetry while matching
  the UI's existing per-session sibling-route pattern (`/sessions/{id}/tasks`).
  `?source=sandbox` selects the read-through; without it (or `source=database`) the handler
  serves the DB rows as a cheap legacy/deployment-agent read. `user_id`/`order`/`limit`/`after`
  apply as on `HandleGetSession`, and the same principal + share-token validation runs first.
- **Read-through flow** (`source=sandbox`, agent resolves to a durable-dir SandboxAgent):
  `EnsureSessionActor` → GET the runtime's local-events endpoint via atenet-router (same
  machinery as `substrate_sandbox_transport.go`) → splice events into the standard response
  envelope → **suspend only if the read woke the actor**. If `EnsureSessionActor` found it
  RUNNING (a chat turn is in flight), leave the lifecycle to the chat path's own
  suspend-on-close — an unconditional suspend would checkpoint the actor mid-generation. This
  needs one additive field on `sandboxbackend.EnsureResult` (the backend already knows whether it
  resumed, `agent_actor.go:82-92`, but doesn't expose it).
- **Runtime side**: a small route in kagent-adk next to `/health`, enabled only when
  `KAGENT_SESSION_DB_URL` is set, returning the local store's events in the controller wire
  shape. The actor is per-session, so it serves exactly its own session. (This endpoint doubles
  as the future export surface for state-carry/debugging.)
- **Fail loud**: `source=sandbox` is an explicit request — old runtime image (404 from actor),
  no free workers, or actor-unreachable return real errors (502/503 with cause), never a silent
  empty array.
- **Guardrails**: this route is a cost amplifier (each call to a suspended session = cold boot +
  a fresh Data-snapshot upload on suspend). Per-session single-flight and a short cooldown live
  in this handler; a rejected-alternatives note and the eventual zero-cost path (a substrate
  "snapshot browse" API to read durable-dir contents out of a snapshot without waking the actor)
  are in §5.7.
- **`GET /api/sessions/{id}` is unchanged** and stays cheap forever (it is called on every chat
  page load — it must never wake actors). Its `events` array becomes "events as known to the
  controller store": full history for Deployment agents, **legacy rows frozen at cutover** for
  sandbox sessions that predate the switch, empty for new sandbox sessions. Add a cheap
  discriminator (`events_source: "database" | "sandbox"` field or response header) so clients can
  tell "no events" from "events live in the actor".

---

## 5. Implementation plan

### 5.0 Substrate dependency bump

- `go/go.mod`: bump the replace to a `github.com/kagent-dev/substrate` pseudo-version at or past
  upstream `main` `53f9848` (durable dir + readyz — no unplanned extras), or a `v0.0.8` tag if
  one is cut. `make -C go generate` / `go mod tidy`, fix any API drift.
- Cluster side: **rebuild kind-kagent's substrate from upstream `main`** (current rsv2 images
  are a divergent dev-branch build — §3). Other clusters need the substrate helm chart ≥ the
  same commit — call this out in release notes.

### 5.1 Python: `DatabaseSessionService` behind `KAGENT_SESSION_DB_URL`

`python/packages/kagent-adk/src/kagent/adk/_a2a.py`, in `KAgentApp.build` (~line 94):

```python
session_db_url = os.getenv("KAGENT_SESSION_DB_URL")
if not local:
    token_service = KAgentTokenService(self.app_name)
    http_client = httpx.AsyncClient(...)                       # unchanged: tasks/memory/tokens
    if session_db_url:
        session_service = DatabaseSessionService(db_url=session_db_url)
    else:
        session_service = KAgentSessionService(http_client)
```

Notes:
- `from google.adk.sessions import DatabaseSessionService` — creates its tables on first use;
  the sqlite file appears under `/data` on first message.
- This is deliberately a **separate, orthogonal code path**: `KAgentSessionService` is not
  touched, and the two implementations share nothing but the `BaseSessionService` interface and
  the single selection point above. Guard against semantic drift (user-id scoping, state-delta
  merging, event ordering) with one **conformance test suite run against both services**.
- Task store, memory service, token service wiring untouched (the `httpx` client stays for them).
- **Local-events endpoint** (for §4.5): register a route next to `/health` (`_a2a.py:181`) —
  e.g. `GET /local/sessions/{id}/events` — only when `KAGENT_SESSION_DB_URL` is set. It reads
  from the local store and returns events in the controller's wire shape
  (`{id, data, created_at}` rows, `order`/`limit` honored). 404 when the env var is unset, so an
  old/misconfigured image fails loud at the controller (§4.5).
- Unit tests: env set → `DatabaseSessionService` with a tmpdir sqlite; multi-append + get
  round-trip; local-events endpoint returns the appended events in order.

### 5.2 Go: volume, mount, readyz, env in the SandboxAgent ActorTemplate

`go/core/pkg/sandboxbackend/substrate/agent_lifecycle.go` (`buildSandboxAgentActorTemplate`,
~line 53, and the spec assembly it feeds in `lifecycle_actortemplate.go:106-191`):

```go
const (
    durableDataVolume   = "data"
    durableDataMount    = "/data"
    sessionDBURLEnv     = "KAGENT_SESSION_DB_URL"
    sessionDBURL        = "sqlite:////data/sessions.db"
    sessionStoreAnno    = "kagent.dev/session-store" // staging gate (M0–M2), opt-out post-M3 — §4.4
)

// when enabled (gated during M0–M2, default for python-runtime SandboxAgents at M3):
spec.Volumes = []atev1alpha1.Volume{{Name: durableDataVolume,
    VolumeSource: atev1alpha1.VolumeSource{DurableDir: &atev1alpha1.DurableDirVolumeSource{}}}}
container.VolumeMounts = []atev1alpha1.VolumeMount{{Name: durableDataVolume, MountPath: durableDataMount}}
container.Readyz = &atev1alpha1.ContainerReadyz{HTTPGet: &atev1alpha1.HTTPGetAction{Path: "/health", Port: substrateKagentListenPort}}
container.Env = append(container.Env, atev1alpha1.EnvVar{Name: sessionDBURLEnv, Value: sessionDBURL})
```

- Readyz uses the existing `/health` route (`_a2a.py:181`) on port 80 — mandatory before the
  `onCommit: Data` flip (Data-resume returns before bootstrap otherwise).
- Table-driven tests in `agent_lifecycle_test.go`: gate on/off × declarative-python/go/BYO
  (env var set only for python-runtime declarative agents until the Go store lands — §4.4).

### 5.3 Flip `onCommit: Data`

One-line change where `SnapshotsConfig` is built (`lifecycle_actortemplate.go:183`):
`OnPause: Full, OnCommit: Data` for templates with the durable-dir volume. Gate behind the same
annotation until measured (§7.10). Templates without the volume keep `Full/Full` — a Data
snapshot with no durable-dir volume is an atelet error.

### 5.4 Stable identity: stable Secret, stable template, shape-hash actor id (§4.2)

All in `go/core/pkg/sandboxbackend/substrate/` + the translator; no substrate changes:

- `agents_backend.go` (`buildSandboxAgentConfigSecret`): stop cloning the config Secret under a
  per-hash name — one stable Secret per agent (e.g. `<agent>-sandbox-config`), contents updated
  in place. `kagentAgentSecretEnv` references the stable name.
- `agent_lifecycle.go` (`buildSandboxAgentActorTemplate`): template name without the config hash
  (one template per agent per *shape*). Compute a **shape hash** over the rendered
  `ActorTemplateSpec` (image digest, command, literal env, mounts, snapshotsConfig) and fold it
  into the template name + a `kagent.dev/shape-hash` annotation, so shape changes still fan out
  blue-green while soft changes don't. (ActorTemplate spec is immutable — a changed spec *must*
  be a new template.)
- `agent_actor.go` (`SandboxAgentSessionActorID`): key the actor id on the shape hash instead of
  the config hash → soft config changes resolve to the **same** actor.
- Reaper (`reapOrphanedSessionActors`): unchanged in spirit — it now only fires on shape
  rollovers (its reason to exist shrinks but doesn't disappear).
- Translator: ensure model/API-key env reaches the substrate backend as `secretKeyRef` (never
  literal values), so key rotation stays a soft change (§4.2 corollary).
- Rollout semantics: a soft config change updates the Secret in place — no template churn, no
  golden retake, no actor churn. An actor mid-turn finishes on the old config; every subsequent
  turn cold-boots onto the new config (same freshness model as a Deployment rollout mid-request).

### 5.5 Controller: `GET /api/sessions/{id}/events?source=sandbox` (read-through — §4.5)

- **Route**: `s.router.HandleFunc(APIPathSessions+"/{session_id}/events", …).Methods(http.MethodGet)`
  next to the existing POST (`server.go:249`). Handler in `handlers/sessions.go` sharing the
  principal/share validation of `HandleGetSession`.
- **`source=database` / absent**: serve DB rows (same code as today's event listing) — cheap
  path for Deployment agents and legacy rows.
- **`source=sandbox`**: resolve the session's agent → must be a substrate SandboxAgent with
  durable-dir sessions (else 400). Then:
  1. `EnsureSessionActor(sa, sessionID)` — extend `sandboxbackend.EnsureResult` with
     `WasRunning bool` (additive; the backend already computes it, `agent_actor.go:82-92`).
  2. GET the runtime's local-events endpoint through atenet-router
     (`newSubstrateAgentRoundTripper`, same as the A2A path).
  3. On completion: `SuspendSessionActor` **only if `!WasRunning`** (§4.5 — never checkpoint a
     mid-generation actor; residual overlap race is the same class as two concurrent chats,
     noted in §6).
  4. Map failures loud: actor 404 (old image / env unset) → 502 with cause; `ErrNoFreeWorkers`
     → 503.
- **Guardrails**: per-session single-flight (`golang.org/x/sync/singleflight` or a small mutex
  map) + short cooldown; each call on a suspended session costs a cold boot and uploads a fresh
  Data snapshot (§6).
- **UI**: one new action `getSessionEvents(sessionId, {source: "sandbox"})` following the
  `getSessionTasks` pattern — used only where events are actually wanted; no change to existing
  session-access code paths.
- **`HandleGetSession` discriminator** (§4.5): add `events_source: "database" | "sandbox"` to
  the response (or a response header) so clients can tell "no events" from "events live in the
  actor". One-field change, no behavior change.
- Tests: handler unit tests with a fake backend (WasRunning true/false → suspend called/not
  called); e2e in the runbook (§7.5).

### 5.6 Optional: one-time backfill from postgres (migration aid)

On boot with an **empty** sqlite and `KAGENT_URL` set, fetch `GET /api/sessions/{id}` once and
seed the local DB. Covers sessions that predate the cutover (their events are in postgres) so
they keep LLM context. Skip unless pre-existing-session continuity matters at rollout time —
alpha likely tolerates "old sessions restart context" (UI history is unaffected either way,
it's task-backed).

### 5.7 Explicit non-goals, follow-ups, rejected alternatives

- **Go declarative runtime**: `go/adk` has only in-memory + HTTP session services
  (`go/adk/pkg/session/session.go` uses the same `/api/sessions` endpoints). Follow-up: a local
  store honoring the same env-var contract. Until it lands the controller does not set the env
  var for `runtime: go` agents — they stay on HTTP sessions (§4.4).
- **BYO images** (langgraph/crewai): they already manage their own state (e.g. `lg_checkpoint`
  tables in kagent postgres). They can get the `/data` mount as a convention later and point
  their own checkpointers at it; no kagent code change now.
- Old-event GC in postgres after cutover; audit any new event-table readers; delete the dead
  event helpers (§2).
- **Rejected: header-based read-through signal** (e.g. `X-Kagent-Events-Source`) — selecting a
  data source that cold-boots an actor is an expensive, side-effectful read; hiding it in a
  header is undiscoverable, invisible in access logs, cache-hostile (`Vary`), and inconsistent
  with the endpoint family's existing query-param options. Query param on a dedicated
  sub-resource won (§4.5).
- **Rejected: dual-write events to postgres** — keeps the write path this plan retires and
  leaves two half-authoritative stores.
- **Rejected: in-actor session-API shim/proxy** (serve `/api/sessions` locally, proxy the rest)
  — only justified under a "zero runtime changes" constraint that was dropped; strictly more
  machinery than the env-var-selected service.
- **Substrate follow-up ask**: a "snapshot browse" API (read durable-dir contents out of a
  stored snapshot) — for a SUSPENDED actor the last Data snapshot is byte-identical to what
  resume-read-suspend returns, so this would make §4.5 reads free and churn-less. File it;
  don't block on it.

---

## 6. Risks / open questions

1. **Durable-dir ownership vs non-root images.** atelet `MkdirAll(volPath, 0o700)` as its own
   user; the slim python image runs non-root (distroless `nonroot`, uid 65532). If the mount is
   not writable inside the sandbox, sqlite fails on first write. **Validate first** (§7.2 step 5)
   — this is the plan's kill-switch risk. Mitigation if it bites: substrate chowns to the
   container user, or the image runs as root inside gVisor (sandboxed anyway).
2. **sqlite availability in the standalone python build.** python-build-standalone bundles
   `sqlite3`, but verify inside the actual actor (§7.2 step 5), given the distroless base has no
   system libsqlite3.
3. **Data-scope golden.** `onCommit` also governs the template's golden snapshot. With `Data`,
   new actors likely cold-start from the OCI image instead of forking a warm Full golden —
   first-message latency for *new* sessions changes. Measure (§7.10); if unacceptable, ask
   substrate for per-operation scope override (golden=Full, suspend=Data).
4. **Per-turn cold boots.** With `onCommit: Data`, *every* message resume is a cold boot
   (python import + readyz). Measure against today's Full-restore. Note the fallback is lopsided:
   staying on `onCommit: Full` keeps warm resumes and suspend/resume durability but **gives up
   rollout survival entirely** (Full-resume restores old memory and no other config-refresh
   channel exists upstream — §4.2). Real latency mitigations: delay the post-turn suspend
   (keep-warm window) or a pause(Full)/suspend(Data) two-tier lifecycle later.
5. **User-id scoping.** `DatabaseSessionService` keys sessions by `(app_name, user_id, id)`.
   Shared sessions (share tokens) must resolve to the same user id the session was created with,
   or lookups miss. Test §7.9; if broken, normalize the user id for sandbox sessions.
6. **sqlite crash consistency.** fscheckpoint copies the db mid-life; the process is stopped
   during checkpoint so the file is quiescent, and `-wal`/`-shm` siblings live in the same dir.
   Low risk; the failure-injection test (§7.11) covers torn state.
7. **Worker crash between suspends** loses the in-flight turn (durable dir persists at suspend
   granularity). Same blast radius as today's in-actor artifacts; events-wise slightly weaker
   than per-event HTTP writes. Documented semantics: state = as of last completed turn.
8. **ListActors is eventually consistent** — runbook checks use `GetActor` for authoritative
   reads.
9. **Read-through vs in-flight chat.** The events read (§4.5) must never suspend an actor it
   didn't wake (`WasRunning` rule). A residual race remains — read wakes the actor, a chat
   arrives mid-read, the read's suspend fires while the chat streams — but it is the same class
   as two concurrent chats to one session today. If it ever matters, the general fix is a
   per-actor in-flight refcount shared by both paths (suspend only at zero); noted, not built.
10. **Read-through snapshot churn.** Every `source=sandbox` read of a suspended session
    cold-boots the actor and, on suspend, uploads a fresh (content-identical) Data snapshot to
    rustfs. KB-scale but unbounded with repeated views — the single-flight/cooldown (§5.5)
    bounds the rate; the runbook checks superseded snapshot objects don't accumulate (§7.12).
    The real fix is the substrate "snapshot browse" ask (§5.7).
11. **Legacy events dual meaning.** For sandbox sessions predating the cutover,
    `GET /api/sessions/{id}` returns their old postgres rows frozen in time, while new events
    live in-actor. The `events_source` discriminator (§4.5) plus documentation cover it; the
    postgres GC follow-up (§5.7) eventually removes it.
12. **Schema migrations for long-running agents.** No in-actor migration is needed under this
    design — the §4.1 invariant pins DB schema to the image digest, and image changes reset the
    store. The exposure is confined to futures that carry state across image versions (§4.1,
    §5.7: JSON-wire transport only, never raw sqlite files). Conversely, controller-side
    postgres migrations (session/task tables) run at kagent upgrade exactly as today and cannot
    strand a suspended actor — actors never talk to postgres. Runbook: after a shape change
    (§7.7.9), verify the new actor's sqlite carries `adk_internal_metadata.schema_version` =
    google-adk's latest.

---

## 7. Manual validation runbook — kind-kagent

Run top to bottom. Every step lists the command and the expected result. Context: single-node
kind cluster `kind-kagent`, substrate in `ate-system` (images `localhost:5001/*:rsv2`), kagent in
`kagent`, DB `kagent-postgresql`, snapshot store rustfs (S3-compatible) behind
`gs://ate-snapshots`, local registry `localhost:5001`.

### 7.0 Tooling prelude

```bash
# ate-api gRPC access (actors are NOT CRDs; only ActorTemplates are)
TOKEN=$(kubectl -n kagent create token kagent-controller --audience api.ate-system.svc --duration 2h)
kubectl -n ate-system port-forward svc/api 8443:443 &
alias ate='grpcurl -insecure -H "authorization: Bearer $TOKEN" localhost:8443'
# usage: ate ateapi.Control/ListActors ; ate -d '{"actorId":"..."}' ateapi.Control/GetActor
# NOTE: ListActors is eventually consistent — use GetActor for authoritative checks.

# postgres shell
alias kpsql='kubectl exec -n kagent deploy/kagent-postgresql -- sh -c \
  "PGPASSWORD=\$POSTGRES_PASSWORD psql -U kagent -d kagent -c"'

# durable-dir host path (hostPath shared with atelet). Two ways in:
kubectl -n ate-system exec ds/atelet -- ls /var/lib/ateom-gvisor/actors/   # primary
docker exec kagent-control-plane ls /var/lib/ateom-gvisor/actors/          # fallback (kind node)

# rustfs snapshot bucket (creds in the rustfs secret in ate-system)
kubectl -n ate-system get secret | grep -i rustfs   # then:
kubectl run -n ate-system s3ls --rm -i --image=amazon/aws-cli --env AWS_ACCESS_KEY_ID=... \
  --env AWS_SECRET_ACCESS_KEY=... -- s3 ls s3://ate-snapshots/kagent/ --recursive --endpoint-url http://rustfs:9000
```

### 7.0a Full-component rebuild on kind-kagent (allowed & expected — do this first)

Rebuilding **every** component on kind-kagent is in scope for this validation. The cluster
currently runs a divergent substrate dev-branch build (`rsv2`, includes unplanned secret-volume
code), so the substrate rebuild is **mandatory** before any milestone testing; the rest rebuild
as their code changes land.

**1. Substrate (control plane + worker runtime)** — from `/Users/jm/Codebase/kagent-substrate/substrate`
at upstream `main` ≥ `53f9848`. Build **all components from the same commit** — mixing versions
causes proto drift (seen before as `RunWorkload → InvalidArgument: missing sandbox_assets`).

```bash
cd /Users/jm/Codebase/kagent-substrate/substrate && git checkout <main-commit>
# Control plane: ko-builds ateapi/atecontroller/atelet/podcertcontroller/atenet to
# localhost:5001 and applies CRDs + kind kustomize overlay onto the CURRENT cluster:
hack/install-ate-kind.sh
# Worker runtime image (not in build-images target):
KO_DOCKER_REPO=localhost:5001 KO_DEFAULTPLATFORMS=linux/$(go env GOARCH) \
  ko build --base-import-paths ./cmd/ateom-gvisor
# Point the worker pool at it (or the matching kagent helm value), then roll the workers:
kubectl -n kagent patch workerpool kagent-default --type merge \
  -p '{"spec":{"ateomImage":"localhost:5001/ateom-gvisor@<digest>"}}'
```

Gotchas (learned on this machine/cluster):
- Do **NOT** run `hack/create-kind-cluster.sh` — `kind create` wedges here; install onto the
  existing cluster only.
- After swapping atelet, **bounce `ate-system/ate-api-server-deployment` AND
  `kagent/kagent-default-deployment`** (workers) — otherwise Run RPCs don't route to the new
  atelet and goldens hang in `ResumeGoldenActor`.
- atelet must run as **root** (nonroot uid 65532 gets `permission denied` on
  `/var/lib/ateom-gvisor/...`); the stock manifests handle this — only hand-rolled images bite.
- Existing goldens/actors/snapshots were produced by the old build — **delete and recreate the
  test SandboxAgents** after the rebuild rather than trusting old goldens (snapshot manifests
  pin runsc versions).
- Verify afterwards: CRD has `volumes[].durableDir` + `containers[].readyz` and **no**
  `volumes[].secret` (proves upstream-main build); `ate-system` pod images are the new digests;
  one chat against a recreated sandbox agent works end-to-end.

**2. kagent controller** — `CONTROLLER_IMAGE_TAG=<fresh-unique-tag> make build-controller` →
`kubectl -n kagent set image deploy/kagent-controller '*=localhost:5001/.../controller:<tag>'`.
Fresh never-used tag every respin (node caches stale tags); verify the **running
(non-Terminating)** pod's `containerStatuses[0].imageID` against the pushed digest.

**3. kagent python runtime** — `KAGENT_ADK_IMAGE_TAG=<fresh> make build-kagent-adk` (and the
`-full` variant if testing SRT agents); wire the digest into the controller's default workload
image per `docs/substrate-python-byo-manual-validation.md`.

**4. kagent helm** — `make helm-install` (or targeted `helm upgrade`) when chart-level values
change (worker `ateomImage`, controller defaults).

**5. UI** — rebuild the `ui/` image and roll `kagent-ui` only when the §5.5 UI action lands;
nothing else in this plan touches the UI.

After any rebuild round, rerun the §7.1 preflight to re-baseline.

### 7.1 Preflight

1. `kubectl config current-context` → `kind-kagent`.
2. CRD supports the feature:
   `kubectl get crd actortemplates.ate.dev -o jsonpath='{.spec.versions[0].schema.openAPIV3Schema.properties.spec.properties.volumes.items.properties}'`
   → contains `durableDir`. Containers schema contains `volumeMounts` and `readyz`.
3. Substrate images: `kubectl -n ate-system get pods -o jsonpath='{range .items[*]}{.spec.containers[*].image}{"\n"}{end}'`
   → all `localhost:5001/*` images built from upstream `main` ≥ `53f9848` (rebuild/redeploy
   first if the cluster still runs the divergent `rsv2` dev-branch build — §7.0a).
4. **Baseline** postgres counts (compare later):
   `kpsql "SELECT count(*) FROM event;"` and
   `kpsql "SELECT count(*) FROM task;"` — record both numbers.

### 7.2 Build, deploy, and spike the two kill-switch risks

1. Bump substrate in `go/go.mod` (§5.0); implement §5.1–§5.2.
2. Controller: `CONTROLLER_IMAGE_TAG=ddir1 make build-controller` → pushes to `localhost:5001`.
   **Use a fresh, never-used tag every respin** (stale-tag node cache), then
   `kubectl -n kagent set image deploy/kagent-controller '*=localhost:5001/kagent-dev/kagent/controller:ddir1'`.
   Verify the **running (non-Terminating)** pod's `containerStatuses[0].imageID` matches the
   fresh digest.
3. Python image: `KAGENT_ADK_IMAGE_TAG=ddir1 make build-kagent-adk` (see
   `docs/substrate-python-byo-manual-validation.md` for the image/digest wiring into the
   controller's default workload image).
4. Create the test agent (declarative **python** runtime), annotated:

   ```yaml
   apiVersion: kagent.dev/v1alpha2
   kind: SandboxAgent
   metadata:
     name: ddir-test
     namespace: kagent
     annotations:
       kagent.dev/session-store: durable-dir
   spec:
     declarative:
       modelConfig: default-model-config
       systemMessage: "You are a helpful assistant. Answer concisely."
     # substrate/worker settings as in existing samples (currency-substrate)
   ```

   Wait `Ready=True`, then verify the rendered template:
   `kubectl -n kagent get actortemplate ddir-test -o yaml` → `spec.volumes[0].durableDir: {}`,
   container mounts `/data`, `readyz.httpGet={path:/health,port:80}`, env
   `KAGENT_SESSION_DB_URL=sqlite:////data/sessions.db`, `snapshotsConfig` per current phase
   (`Full/Full` first; `onCommit: Data` after §7.10's first pass), and `status.goldenSnapshot`
   populated.
5. **Spike (risks #1/#2)** — send one message (step 7.3.1) and immediately check the actor's log
   for sqlite errors, then confirm the file exists:
   `kubectl -n ate-system exec ds/atelet -- sh -c 'find /var/lib/ateom-gvisor/actors -path "*durable-dir*" -name "sessions.db*" -ls'`
   → `sessions.db` present, non-empty, under `…:{templateName}:asr-…/durable-dir/data/`.
   If write fails (permissions / missing sqlite): **stop**, resolve risk #1/#2 before continuing.

### 7.3 Core session continuity (suspend/resume per turn)

Chat via the UI (`kubectl port-forward -n kagent svc/kagent-ui 3000:8080`) **or** raw A2A:

```bash
SESS=ddir-sess-1
curl -s http://localhost:8083/api/a2a-sandboxes/kagent/ddir-test \
  -H 'content-type: application/json' -H 'X-User-Id: jm@solo.io' -d '{
  "jsonrpc":"2.0","id":"1","method":"message/send","params":{"message":{
    "messageId":"m1","role":"user","contextId":"'$SESS'",
    "parts":[{"kind":"text","text":"My favorite color is teal. Remember it."}]}}}'
```

1. Message 1 sends and completes. `ate -d '{"actorId":"<asr-…-'$SESS'>"}' ateapi.Control/GetActor`
   (id format: `asr-<ns>-<name>-<configHash>-<sessionID>`, sha256-shortened when long — grab the
   exact id from controller logs or `ListActors`).
2. After the response body closes, the actor transitions to **SUSPENDED** (kagent suspends
   per-turn). Poll `GetActor`.
3. Message 2: *"What is my favorite color?"* → **"teal"**. This proves: resume happened, the
   durable dir came back, and the runner rebuilt context **from the local sqlite** (step 7.4
   proves postgres was not involved).
4. Repeat for 5+ turns referencing earlier turns ("what was my first message?") — deep history
   intact, one suspend/resume cycle per turn.
5. Copy the db out and inspect rows:
   `kubectl -n ate-system cp $(kubectl -n ate-system get pod -l <atelet-selector> -o name | cut -d/ -f2):/var/lib/ateom-gvisor/actors/<dir>/durable-dir/data/sessions.db /tmp/s.db`
   then locally `sqlite3 /tmp/s.db '.tables' 'select id, count(*) from events group by 1;'`
   → ADK tables (`sessions`, `events`, `app_states`, `user_states`), event count == turns×2-ish.

### 7.4 Postgres negative proof

1. `kpsql "SELECT count(*) FROM event e JOIN session s ON e.session_id=s.id WHERE s.agent_id LIKE '%ddir_test%';"`
   → **0** (the headline assertion).
2. Global event count unchanged from the §7.1 baseline (± other agents' traffic).
3. `kpsql "SELECT id,user_id,agent_id FROM session WHERE agent_id LIKE '%ddir_test%';"`
   → session **row exists** (metadata stays).
4. `kpsql "SELECT count(*) FROM task WHERE session_id='<sess>';"` → == number of turns (tasks stay).
5. Control: chat with a **non-annotated** substrate agent → its event rows still appear
   (opt-in isolation works, no regression for others).

### 7.5 UI behavior & the events read-through

1. Open the session in the UI → full history renders (task-backed), token stats populated.
2. Hard-refresh mid-session → history reloads.
3. Session appears in the sidebar list; rename and delete flows work (delete → §7.12).
4. Session **share link**: open as anonymous/other user → history renders, `read_only` honored
   (this exercises the only UI path that reads the events endpoint — expect it to work with an
   empty events array).
5. `GET /api/sessions/{id}` for the sandbox session → 200, session row present, `events` empty,
   discriminator indicates in-actor storage (§4.5). Confirm this call does **not** wake the
   actor (`GetActor` stays SUSPENDED).
6. **Read-through happy path**: with the actor SUSPENDED, call
   `curl "http://localhost:8083/api/sessions/$SESS/events?source=sandbox" -H 'X-User-Id: jm@solo.io'`
   → full chronological events (matches the sqlite copy from §7.3.5). Verify via `GetActor`:
   actor resumed during the call and is **SUSPENDED again** shortly after it completes.
7. **Read-through does not kill a live chat**: start a slow streaming message, and mid-stream
   call the events endpoint → events return, the stream completes normally, and the actor
   suspends only after the chat's own response closes (the `WasRunning` rule).
8. **Fail-loud checks**: point the agent at an image without the runtime endpoint (or unset the
   env var) → `source=sandbox` returns 502 with a clear cause, not an empty array. Scale the
   worker pool to zero free workers → 503.
9. `source=database` (or param absent) on the same route → returns the DB rows (legacy/frozen
   for this agent), actor untouched.
10. Repeat step 6 five times in a row → each read works; note the per-read latency (cold boot)
    and check rustfs for snapshot-object churn (feeds §7.12.2 and risk #10).

### 7.6 Infrastructure restarts (requirement 2)

After each bullet, send *"what's my favorite color?"* in the **same** session and expect "teal":

1. **Controller restart:** `kubectl -n kagent rollout restart deploy/kagent-controller` (wait Ready).
2. **atelet restart:** `kubectl -n ate-system delete pod -l <atelet ds selector>` (wait Ready).
3. **ate-api restart:** `kubectl -n ate-system rollout restart deploy/ate-api-server-deployment`.
4. **Worker restart while actor SUSPENDED** (the normal between-turns state):
   `kubectl -n kagent delete pod -l kagent.dev/worker-pool=kagent-default` — durable dir was
   already committed to rustfs at suspend; resume lands on the new worker and restores it.
5. **postgres restart** while chatting — proves session flow no longer depends on it. (Note:
   task writes still need the DB, so do this between messages, not mid-stream.)
6. (Optional, destructive) `docker restart kagent-control-plane`, wait for cluster convergence,
   then continue the session — full-node restart survival.

### 7.7 Config rollout continuity (requirement 3 — the headline test)

> Runs after M3 (§5.4 identity + `onCommit: Data`). If testing an earlier milestone (per-hash
> identity still in place): a rollout mints a new actor and the session's LLM context starts
> empty — **expected pre-M3 behavior**, verify it explicitly and confirm UI history (tasks)
> still renders. The steps below are the **end-state** assertions:

1. Note the session's current actor id (`GetActor`), the agent's ActorTemplate name, and the
   config Secret's `resourceVersion`.
2. Change soft config: `kubectl -n kagent patch sandboxagent ddir-test --type merge -p \
   '{"spec":{"declarative":{"systemMessage":"You are a helpful assistant. Always answer in French."}}}'`
3. Wait for reconcile: agent `Ready=True`; the **stable** config Secret's contents changed in
   place (new `resourceVersion`, same name).
4. Same session, ask *"What is my favorite color?"* Expect **both**:
   - answer in **French** (cold-boot resume re-resolved the secret env → new config), and
   - it still says **teal** (session state survived — restored from the durable dir).
5. Assert identity stability: same actor id as step 1; **same single ActorTemplate** (no
   fan-out, no new golden); old actor **not** reaped (nothing to reap); shape-hash
   annotation unchanged.
6. Ask about turn-1 content again — full multi-turn history intact post-rollout.
7. Flip config back (English) → same checks (flip-back is just another rollout).
8. New session post-rollout works and gets the current config.
9. **Shape change** (e.g. force the full image via SRT settings, or add a literal `spec.env`
   var): new template + new actor id ARE expected; document that the session's durable dir does
   **not** carry over (accepted limitation §4.2) and that the orphan reaper cleans the old actor.
10. **API-key rotation** (rotate the ModelConfig secret): must behave as a soft change — same
    actor, next turn works with the new key (verifies the no-literal-env rule in §5.4).

### 7.8 Concurrency & isolation

1. Run two sessions (different `contextId`) against `ddir-test` in parallel; interleave messages.
2. Distinct actors (two `asr-*` ids), distinct `durable-dir` host dirs, distinct `sessions.db`.
3. Cross-check: session A's facts never leak into session B's answers; both survive their own
   suspend/resume cycles.
4. Two agents annotated simultaneously → no volume-name or path collisions.

### 7.9 HITL and shared-session identity

1. On an HITL-capable substrate agent (cf. `hitl-substrate`) with the annotation: trigger an
   approval → task goes `input_required` (actor suspends) → approve from the UI → tool executes
   and the task completes. This proves pending-confirmation events survive the suspend/resume
   through the **local** session store while the task rides postgres.
2. Approve only after a config rollout (end-state only): HITL continuation must still match the
   pending function call from the restored durable dir.
3. Shared session (risk #5): create a share link, send a message as a different user →
   the runner must find the session in sqlite (user-id scoping). If it misses (fresh empty
   context), record it and fix per §6.5.

### 7.10 Latency & snapshot economics (`Full` vs `Data`)

Run once with `onCommit: Full` (phase-1 deploy), then flip to `Data` (§5.3) and re-run:

1. Per-message wall time on an established session:
   `for i in 1 2 3 4 5; do time curl -s …message/send… >/dev/null; done` (resume path dominates).
2. First message of a brand-new session (actor create path — Data-scoped golden cold start,
   risk #3).
3. Suspend duration & snapshot size: rustfs listing (`7.0`) — expect per-suspend objects to
   drop from ~100s of MB (Full) to KBs (Data); atelet logs show `fscheckpoint` vs `checkpoint`.
4. Readiness gating: with `Data`, the first request after resume must **not** get a connection
   error (PR #330 probe holds the resume until `/health` is 200). Hammer resume→request in a
   tight loop 10×.
5. Record the numbers in this doc; decide whether `Data` per-turn latency is acceptable or the
   pause-tier optimization (§4.3) gets scheduled.

### 7.11 Failure injection (documented-semantics checks)

1. **Worker killed while actor RUNNING** (mid-generation): next message → actor recreated/resumed
   from the **last suspend** → state = end of previous completed turn; the killed turn's events
   are absent from sqlite; UI may show the task as failed/stale. Confirm no crash-loop and no
   torn sqlite (db opens, queries fine).
2. **rustfs unavailable during suspend** (`kubectl -n ate-system scale deploy/rustfs --replicas=0`):
   suspend fails → verify kagent surfaces the error and the actor recovers once rustfs returns
   (`--replicas=1`).
3. Pre-existing session from before the cutover (events in postgres, empty durable dir): context
   starts fresh (or backfilled if §5.6 was implemented) — verify whichever was decided; UI
   history intact regardless; `GET …/events?source=database` still returns the frozen legacy
   rows (§4.5).

### 7.12 Deletion & GC

1. Delete the session in the UI →
   - actor gone: `GetActor` → NotFound (allow for eventual consistency),
   - durable dir removed from the host path,
   - session row soft-deleted, task rows handled as today.
2. rustfs: the actor's snapshot objects under `…/kagent/ddir-test/<actorID>/` are GC'd (or
   document if substrate defers this — record what actually happens).
3. `kubectl -n kagent delete sandboxagent ddir-test` → finalizer runs: all ActorTemplates,
   actors, config secrets, durable dirs, snapshots cleaned; nothing left in
   `ListActors`/host path/rustfs for the agent.

### Acceptance checklist

| # | Assertion | Runbook |
|---|---|---|
| 1 | Session events land in `/data/sessions.db`, not in postgres `event` | 7.2.5, 7.4 |
| 2 | Multi-turn context survives per-turn suspend/resume | 7.3 |
| 3 | Context survives controller/atelet/ate-api/worker/postgres/node restarts | 7.6 |
| 4 | Context **and** new config survive a soft config rollout, same actor | 7.7 |
| 5 | UI history, token stats, share links, session list unaffected | 7.5 |
| 6 | `GET …/events?source=sandbox` returns live events; actor suspended after; never suspends a live chat; fails loud | 7.5.6–8 |
| 7 | HITL approve/reject flows work across suspend/resume (and rollout) | 7.9 |
| 8 | Sessions/agents delete cleanly (actor, host dir, snapshots) | 7.12 |
| 9 | Non-gated agents (Deployment, Go runtime, ungated substrate) completely unaffected | 7.4.5 |
| 10 | `Data` snapshot economics measured; latency decision recorded | 7.10 |
| 11 | Failure-mode semantics match §6.7 documentation | 7.11 |

---

## 8. Milestones

Every milestone is validated manually on **kind-kagent**, where rebuilding **all** components
(substrate control plane + worker runtime, kagent controller, python runtime images, helm, UI)
is allowed and expected — §7.0a is the rebuild procedure, and each milestone lists its runbook
slice.

1. **M0 — rebuild + spike (1–2 days):**
   - Execute the **§7.0a full rebuild**: substrate from upstream `main` ≥ `53f9848` (removes
     the divergent `rsv2` dev-branch build and its unplanned secret-volume code), recreate the
     test SandboxAgents, rerun §7.1 preflight.
   - `go/go.mod` substrate dep bump (§5.0).
   - Spike with a **hand-edited ActorTemplate** (no kagent code yet): non-root actor writes
     sqlite in a durableDir and it survives one suspend/resume (§7.2.5, §7.3.3). De-risks
     #1/#2 before any real code.
2. **M1 — storage + read path (gated):** §5.1 (runtime local store + local-events endpoint +
   conformance suite), §5.2 (template wiring), §5.5 (controller `GET …/events?source=sandbox`,
   `EnsureResult.WasRunning`, `events_source` discriminator, UI action) — all behind the
   annotation gate, scopes `Full/Full`. Rebuild: controller + adk images (+ UI for the action).
   Manual validation: runbook §7.1–7.6, §7.8.
3. **M2 — `onCommit: Data` flip (§5.3):** rebuild: controller only. Manual validation:
   §7.10 (latency/snapshot economics — record numbers and make the latency call), §7.11
   (failure injection), re-run §7.3 and §7.5.6–8 under Data scope.
4. **M3 — stable identity + default-on:** §5.4 (stable Secret, stable template, shape-hash
   actor id); **default on** for python-runtime substrate SandboxAgents, annotation becomes
   opt-out (§4.4); decide §5.6 backfill. Rebuild: controller. Manual validation: §7.7 (rollout
   continuity — the headline test), §7.9 (HITL + shared sessions), §7.12 (deletion/GC), full
   acceptance checklist pass.
   **Hard ordering: M3 must not ship before M2** — a stable Secret name under `Full` snapshots
   resurrects the stale-golden hazard the per-hash name deliberately guards against (§4.2);
   and default-on must not precede M3 — durable-dir sessions under per-hash identity regress
   rollout continuity vs postgres (§4.2).
5. **M4 (follow-up):** Go ADK local session store honoring the same env-var contract; extend
   the default to `runtime: go` agents (§5.7). Manual validation: §7.3/§7.7 repeated against a
   `runtime: go` SandboxAgent.
