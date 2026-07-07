# Handoff: substrate durable-dir session state

**Date:** 2026-07-06
**Branch:** `feat/persist-substrate-session-state-to-durable-dir`
**Authoritative design doc:** `SUBSTRATE_DURABLE_DIR_SESSION_PLAN.md` (repo root) — read it
before writing code; this file is the orientation layer on top of it.

## What this work is

Move a substrate SandboxAgent's **ADK session state** (event log + state — what the runner
replays to rebuild LLM context each turn) out of the controller's postgres and into a **sqlite
DB in a substrate `durableDir` volume inside the session's actor**. Decided end state:

- **Default** for substrate SandboxAgents (python runtime first), selected by a
  controller-injected env var `KAGENT_SESSION_DB_URL=sqlite:////data/sessions.db`. Presence =
  switch, value = config, removal = rollback. The runtime keeps a **separate orthogonal code
  path** (`google.adk.sessions.DatabaseSessionService`) beside the untouched HTTP
  `KAgentSessionService` (plan §4.4, §5.1).
- Session **rows** and A2A **tasks stay in postgres** — the UI renders chat from tasks, so UI
  session access does not change (plan §2).
- Events stay API-reachable via a **new controller route**
  `GET /api/sessions/{id}/events?source=sandbox`: resume actor → fetch from a runtime
  local-events endpoint → **suspend only if the read woke it** (plan §4.5, §5.5). Header-based
  signaling was considered and rejected (§5.7).
- Rollout survival comes from **stable identity + Data-scope cold boots**: stable per-agent
  config Secret (updated in place), stable ActorTemplate, actor id keyed by template **shape
  hash** instead of config hash, `snapshotsConfig.onCommit: Data` (plan §4.2, §5.4).

## Current state

- **Design is complete and consolidated in the plan doc. NO implementation code exists yet.**
- The plan doc went through several major revisions in one long session — trust the doc, not
  intermediate reasoning you may find elsewhere.
- Other untracked docs at repo root (`SUBSTRATE_CONFIG_VOLUME_PLAN.md` etc.) are context only;
  the config-volume plan is **unplanned/dead** — do not build on it.

## Load-bearing verified facts (all re-verified against code; refs in the plan)

1. Session events today: runtime `KAgentSessionService` → HTTP → controller → postgres `event`
   table (verified live: 486 rows for `py_decl_substrate`). The controller never parses the
   blob; the runtime is producer *and* sole consumer. UI chat renders from **tasks**.
2. Substrate upstream `main` (`53f9848`, repo at `/Users/jm/Codebase/kagent-substrate/substrate`)
   has durable-dir (`37b5006`) + readyz probes (`125180e`). It does **NOT** have secret volumes
   (`593b302` is dev-branch-only, unplanned). kagent's go.mod pin `kagent-dev/substrate v0.0.7`
   **predates durable-dir** → dep bump required (plan §5.0).
3. **The kind-kagent cluster runs a divergent substrate build** (`localhost:5001/*:rsv2`, from
   the dev branch WITH secret volumes). It must be rebuilt from upstream `main` before
   validation — procedure in plan §7.0a.
4. `ActorTemplate.spec` is **immutable** (CEL `self == oldSelf`). `secretKeyRef` env is
   re-resolved at every `ResumeActor` — through a **30s TTL cache** (`envSecretCacheTTL`).
   The per-hash config-Secret name kagent uses today is a **deliberate golden cache-buster**
   (comment in `buildSandboxAgentActorTemplate`) — hence the hard ordering below.
5. Data-scope resume = cold boot from the OCI image with durable dir restored and env
   re-resolved; readyz gates the resume RPC. Durable-dir data is keyed
   `(ns, templateName, actorID)`; **no cross-actor restore exists** in substrate.
6. `GET /api/sessions/{id}/events` is **free namespace** (only POST registered,
   `server.go:249`). `EnsureSessionActor` knows whether it resumed but doesn't expose it —
   needs an additive `WasRunning` on `sandboxbackend.EnsureResult`.
7. Schema migrations: none needed in-actor **by construction** — the sqlite schema is pinned to
   the digest-pinned image; image change = shape change = fresh DB. google-adk versions its
   schema (`adk_internal_metadata`, v0→v1 already happened) with **offline-only** migration
   tools — so any future cross-shape state carry must move event JSON, never sqlite files
   (plan §4.1 invariant, risk #12).

## Hard ordering constraints (do not violate)

- **M2 (Data flip) before M3 (stable Secret name)** — stable name under `Full` scope
  resurrects the stale-golden bug the per-hash name guards against (§4.2).
- **Default-on not before M3** — durable-dir sessions under today's per-config-hash identity
  *regress* rollout continuity vs postgres (§4.2).
- The `?source=sandbox` handler must implement **suspend-only-if-woken** before it is safe
  (§4.5) — unconditional suspend checkpoints a mid-generation chat.

## Next actions (M0, plan §8)

1. Run the **§7.0a full rebuild** of substrate on kind-kagent from upstream `main`
   (build ALL components from one commit; gotchas are listed in §7.0a — read them, several are
   machine-specific hard-won lessons: never run `hack/create-kind-cluster.sh` here; bounce
   ate-api + workers after swapping atelet; delete/recreate test SandboxAgents after).
2. Bump `go/go.mod` substrate replace to that commit (§5.0).
3. **Spike the two kill-switch risks** with a hand-edited ActorTemplate before writing code:
   non-root actor can write sqlite in a durableDir mount, and it survives suspend/resume
   (§7.2.5, §7.3). If the durable dir isn't writable by uid 65532 (atelet MkdirAll 0700) or
   sqlite is missing, stop and resolve first (risks #1/#2; note `libsqlite3` is bundled at
   `/usr/lib/kagent-libs` in the slim image — encouraging but unverified in-actor).
4. Then M1 per plan §8.

## Cluster & tooling quick reference

- Cluster `kind-kagent`: substrate in `ate-system` (+ rustfs S3 store for snapshots, valkey),
  kagent in `kagent` (controller, `kagent-postgresql`, UI), local registry `localhost:5001`,
  worker pool `kagent/kagent-default` (`WorkerPool.spec.ateomImage`).
- Actor inspection: mint token `kubectl -n kagent create token kagent-controller --audience
  api.ate-system.svc`, port-forward `svc/api` in ate-system, `grpcurl -insecure` against
  `ateapi.Control` (`GetActor` is authoritative; `ListActors` is eventually consistent).
  Full prelude in plan §7.0.
- Controller deploys: fresh unique image tag every respin; verify the running
  (non-Terminating) pod's imageID. Build: `CONTROLLER_IMAGE_TAG=x make build-controller`;
  python image: `KAGENT_ADK_IMAGE_TAG=x make build-kagent-adk`.
- Durable-dir host path: `/var/lib/ateom-gvisor/actors/{ns}:{template}:{actorID}/durable-dir/`
  — inspect via the atelet DaemonSet pod or `docker exec kagent-control-plane`.
- Session actor id today: `asr-<ns>-<name>-<configHash>-<sessionID>`; per-turn lifecycle:
  resume on message, suspend on response close (`substrate_sandbox_transport.go`).
- Persistent memory notes exist for this project (auto-loaded): `durable-dir-session-plan`,
  `substrate-actor-inspection`, `controller-test-deploy-kind`, `substrate-atelet-testing`,
  `substrate-byo-restore-safe-python`.

## Open decisions (flag to the user when relevant)

- §5.6 backfill (seed local DB from postgres for pre-cutover sessions): leaning skip for alpha;
  decide at M3.
- Data-scope per-turn cold-boot latency: measure at M2 (§7.10) and make the acceptance call;
  mitigations sketched in §4.3/risk #4.
- Read-through guardrail values (single-flight/cooldown) — pick during §5.5 implementation.
- Substrate follow-up asks to file upstream: "snapshot browse" (read durable-dir from a
  snapshot without waking the actor) and "seed durable dir from snapshot at CreateActor"
  (cross-shape state carry). Neither blocks this plan.
