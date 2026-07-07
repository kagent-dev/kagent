# Manual validation: durable-dir session state for substrate SandboxAgents

Validates that a substrate SandboxAgent's ADK session state (event log the runner replays to
rebuild LLM context) lives in a sqlite DB inside the session actor's `durableDir` volume instead
of the controller's postgres — and that the session-pinned actor model holds: **one session ⇔ one
actor for the session's entire life**, across per-turn suspend/resume, config rollouts, and shape
rollouts.

All steps below were executed successfully on `kind-kagent` (2026-07-06). Every step lists the
command and the expected result. Run top to bottom; later sections assume earlier state.

## Behavior under test (summary)

| Change | Template / golden | Existing session's next message | New session |
|---|---|---|---|
| Config change (systemMessage, modelConfig, API key — anything Secret-borne) | Same template re-applied in place (generation annotation bumps; no golden rebuild) | Resumes its **same actor**; Data-scope cold boot re-resolves the stable Secret → new config + old state | New actor from the same golden |
| Shape change (image digest, command, literal env, scopes) | New template + golden built alongside; the old **Ready** template keeps serving until the new golden is Ready | Resumes its **same actor under its birth template** (old shape, old config env refs, state intact) | New actor born from the **new** template |
Durable-dir session storage is the **only** behavior for `runtime: python` declarative
substrate SandboxAgents (no opt-out). Go-runtime declarative and BYO agents stay on HTTP
sessions (the Go ADK has no local session store yet; BYO images manage their own state).

## 0. Prerequisites & tooling

```bash
kubectl config current-context                     # kind-kagent
# Substrate must be built from kagent-dev/substrate v0.0.8+ (ActorRef/atespace proto).
# The controller image must include this feature branch.

# ate-api gRPC access (actors are NOT CRDs — only ActorTemplates are):
kubectl -n ate-system port-forward svc/api 8443:443 &
TOKEN=$(kubectl -n kagent create token kagent-controller --audience api.ate-system.svc --duration 2h)
actor() {  # actor <actor-id>  → status + template
  grpcurl -insecure -H "authorization: Bearer $TOKEN" \
    -d "{\"actorRef\":{\"atespace\":\"kagent\",\"name\":\"$1\"}}" \
    localhost:8443 ateapi.Control/GetActor
}

# controller API:
kubectl -n kagent port-forward deploy/kagent-controller 8083:8083 &
chat() {  # chat <agent> <session-id> <text>
  curl -sS -m 180 "http://localhost:8083/api/a2a-sandboxes/kagent/$1" \
    -H 'content-type: application/json' -H 'X-User-Id: jm@solo.io' -d '{
    "jsonrpc":"2.0","id":"1","method":"message/send","params":{"message":{
      "messageId":"'"$RANDOM"'","kind":"message","role":"user","contextId":"'"$2"'",
      "parts":[{"kind":"text","text":"'"$3"'"}]}}}'
}

# postgres shell:
kpsql() { kubectl exec -n kagent deploy/kagent-postgresql -- \
  sh -c "PGPASSWORD=\$POSTGRES_PASSWORD psql -U kagent -d kagent -tA -c \"$1\""; }
```

> Model-provider note: the cluster's `default-model-config` (OpenAI) key is quota-exhausted;
> use `google-model-config` (gemini-2.5-flash). The free-tier Gemini key rate-limits under
> rapid testing — a `failed` task with `429 RESOURCE_EXHAUSTED` in its status message is the
> provider, not the feature; wait ~60s and retry. Gemini also sometimes answers memory
> *meta*-questions coyly ("I can't recall previous messages"); prefer direct probes
> ("State my favorite color") over meta ones.

## 1. Create the test agents

```yaml
# ddir-test: durable-dir sessions by DEFAULT (python declarative substrate agent)
apiVersion: kagent.dev/v1alpha2
kind: SandboxAgent
metadata: {name: ddir-test, namespace: kagent}
spec:
  type: Declarative
  declarative:
    modelConfig: google-model-config
    runtime: python
    systemMessage: "You are a helpful assistant. Answer concisely in English, in one short sentence."
  substrate: {workerPoolRef: {name: kagent-default}}
---
# ddir-control: Go runtime — stays on HTTP sessions → postgres (pre-feature behavior),
# proving the durable-dir path is isolated to python declarative agents.
apiVersion: kagent.dev/v1alpha2
kind: SandboxAgent
metadata:
  name: ddir-control
  namespace: kagent
spec:
  type: Declarative
  declarative:
    modelConfig: google-model-config
    runtime: go
    systemMessage: "You are a helpful assistant. Answer concisely in English, in one short sentence."
  substrate: {workerPoolRef: {name: kagent-default}}
```

Wait for `Ready=True` on both (`kubectl -n kagent get sandboxagent -w`; golden build ≈ 30–60s).

**Verify the rendered templates:**

```bash
kubectl -n kagent get actortemplates -l kagent.dev/sandbox-agent=ddir-test -o yaml
```

Expected for `ddir-test-<shapehash>` (name suffix = `kagent.dev/shape-hash` annotation):
- `spec.volumes: [{name: data, durableDir: {}}]`, container mounts `/data`
- `containers[0].readyz: {httpGet: {path: /health, port: 80}}`
- env `KAGENT_SESSION_DB_URL=sqlite+aiosqlite:////data/sessions.db`
- `snapshotsConfig: {onPause: Full, onCommit: Data}`
- config env (`KAGENT_CONFIG_JSON` etc.) via `secretKeyRef` against the **stable** Secret `ddir-test`

Expected for the control agent's template: **none** of the above (no volumes, no readyz, no
session env var, `onCommit: Full`).

## 2. Multi-turn continuity across per-turn suspend/resume

```bash
chat ddir-test sess-1 "My favorite color is teal and my lucky number is 42. Acknowledge briefly."
actor asr-kagent-ddir-test-sess-1        # → STATUS_SUSPENDED within seconds of the response
chat ddir-test sess-1 "What are my favorite color and lucky number?"
```

Expected:
- The actor id is **`asr-kagent-ddir-test-sess-1`** — derived from the session alone, no hash.
- After every response the actor transitions to `STATUS_SUSPENDED` (poll `actor …`).
- Turn 2 answers **teal / 42**: the Data-scope resume cold-booted the actor, restored the
  durable dir, and the runner rebuilt LLM context from the local sqlite.
- Repeat for more turns; "Repeat back exactly what my first message said" returns turn 1 verbatim.

## 3. Postgres proofs (the headline)

```bash
kpsql "SELECT count(*) FROM event e JOIN session s ON e.session_id=s.id WHERE s.agent_id LIKE '%ddir_test%';"
#  → 0, always, no matter how many turns you run                 (events live in the actor)
kpsql "SELECT id,user_id FROM session WHERE agent_id LIKE '%ddir_test%';"
#  → one row per session (created by the controller on the A2A path — headless sessions included)
kpsql "SELECT count(*) FROM task WHERE session_id='sess-1';"
#  → one per turn                                                 (tasks stay in postgres; UI history)

chat ddir-control ctrl-1 "Say hello."
kpsql "SELECT count(*) FROM event e JOIN session s ON e.session_id=s.id WHERE s.agent_id LIKE '%ddir_control%';"
#  → grows per turn                                (non-durable-dir agents still write to postgres)
```

## 4. Events read-through: `GET /api/sessions/{id}/events?source=sandbox`

```bash
# With the actor SUSPENDED:
curl -sS "http://localhost:8083/api/sessions/sess-1/events?source=sandbox&user_id=jm@solo.io&order=asc" \
  -H 'X-User-Id: jm@solo.io' | python3 -m json.tool | head -40
```

Expected:
- Full chronological event rows (`{id, data, created_at}`; `data` parses as an ADK Event).
- The call **resumes** the actor and **suspends it again** afterwards (`actor …` → SUSPENDED
  shortly after). If a chat is streaming when you call it, the read must NOT suspend the actor
  (suspend-only-if-woken).
- `source=database` (or no `source`) on the same route returns the postgres rows instead
  (empty for durable-dir sessions, "frozen legacy" for pre-cutover ones).
- Concurrent calls for the same session share ONE resume→fetch→suspend cycle (single-flight);
  sequential calls each re-wake the actor and always return current state (no caching).
- `GET /api/sessions/sess-1` (no source) stays cheap, never wakes the actor, and carries
  `events_source: "sandbox"` so clients can tell "no events" from "events live in the actor".

## 5. Config rollout: same actor, new config, state intact

```bash
actor asr-kagent-ddir-test-sess-1        # note actorTemplateName
kubectl -n kagent get secret ddir-test -o jsonpath='{.metadata.resourceVersion}'   # note rv

kubectl -n kagent patch sandboxagent ddir-test --type merge \
  -p '{"spec":{"declarative":{"systemMessage":"You are a helpful assistant. Always answer in French, in one short sentence."}}}'

kubectl -n kagent get actortemplates -l kagent.dev/sandbox-agent=ddir-test \
  -o custom-columns='NAME:.metadata.name,GEN:.metadata.annotations.kagent\.dev/desired-generation'
#  → SAME single template, generation bumped in place — NO new template, NO golden rebuild
kubectl -n kagent get secret ddir-test -o jsonpath='{.metadata.resourceVersion}'
#  → rv changed: stable Secret updated in place

sleep 40   # ate-api resolves secretKeyRef through a ~30s cache; a resume inside the window may
           # legitimately boot the old config once — this is the documented eventual consistency
chat ddir-test sess-1 "What are my favorite color and lucky number?"
```

Expected: the answer is **in French** (new config, re-resolved at the cold-boot resume) and
still says **teal / 42** (old state) — on the **same actor id** (`actor …` confirms). Flip the
systemMessage back and repeat: rollouts are repeatable on the same actor.

## 6. Shape rollout: old sessions pinned, new sessions on the new shape

A literal env var is spec-visible → shape change:

```bash
kubectl -n kagent patch sandboxagent ddir-test --type merge \
  -p '{"spec":{"declarative":{"deployment":{"env":[{"name":"TEST_SHAPE","value":"one"}]}}}}'

kubectl -n kagent get actortemplates -l kagent.dev/sandbox-agent=ddir-test \
  -o custom-columns='NAME:.metadata.name,PHASE:.status.phase,GEN:.metadata.annotations.kagent\.dev/desired-generation'
#  → TWO templates now: old one retained (still Ready), new ddir-test-<newhash> building → Ready.
#    While the new golden builds, all traffic keeps landing on the old Ready template (blue-green).
```

After the new template is Ready:

```bash
chat ddir-test sess-1 "State my favorite color and lucky number."
actor asr-kagent-ddir-test-sess-1
#  → answers teal/42; actorTemplateName is STILL the OLD template (birth template): the session
#    is pinned — substrate rebuilds the workload spec from the actor's own template on resume.

chat ddir-test sess-2 "Do you know my favorite color? Answer yes or no."
actor asr-kagent-ddir-test-sess-2
#  → clean state (knows nothing), actorTemplateName = the NEW template.
```

This is the accepted trade: pinned sessions never receive image-borne changes (only Secret-borne
config); they stay on their birth shape until deleted.

## 7. Session deletion (single-id cleanup)

```bash
curl -sS -X DELETE "http://localhost:8083/api/sessions/sess-1?user_id=jm@solo.io" -H 'X-User-Id: jm@solo.io'
actor asr-kagent-ddir-test-sess-1        # → NotFound (allow a few seconds)
```

One deterministic actor id per session → one delete, regardless of how many rollouts the
session lived through. Deleting the SandboxAgent removes all templates, actors, secrets, and
snapshots (finalizer).

## 8. Latency & snapshot economics (measured 2026-07-06, single-node kind)

| Metric | `onCommit: Full` (pre-flip) | `onCommit: Data` (shipped) |
|---|---|---|
| Per-turn wall time (short prompt, incl. LLM) | 3.9–4.6 s | 12.6–16.3 s (cold boot + readyz dominates) |
| Per-suspend upload to rustfs | ~67 MB | **~5 KB** (sqlite content; grows with history) |

Verify snapshot sizes yourself (rustfs is S3-compatible; creds in the rustfs deployment env):

```bash
kubectl -n ate-system port-forward svc/rustfs 9000:9000 &
AWS_ACCESS_KEY_ID=… AWS_SECRET_ACCESS_KEY=… aws s3 ls \
  "s3://ate-snapshots/kagent/ddir-test/asr-kagent-ddir-test-sess-1/" --recursive \
  --endpoint-url http://localhost:9000
```

Data resumes being cold boots is the cost of config-refresh-on-resume; keep-warm windows or a
pause tier are the noted future mitigations if per-turn latency matters.

## 9. Known environmental gotchas

- **Durable-dir contents are not visible on the worker host path** — `fscheckpoint` captures
  them straight into the snapshot. Verify state via the read-through endpoint (§4), not
  `find /var/lib/ateom-gvisor/...`.
- The atelet/ateapi images are distroless; inspect host paths via
  `docker exec kagent-control-plane …`, not `kubectl exec ds/atelet`.
- An actor suspended under `Full` scope cannot resume under a template flipped to `Data`
  (cross-scope restore doesn't exist). This can only be hit by upgrading a pre-Data cluster
  with live sessions; session pinning prevents it going forward.
- Unit tests covering this feature: `go test ./core/pkg/sandboxbackend/substrate/ ./core/internal/httpserver/handlers/ ./core/internal/a2a/`
  and `uv run --package kagent-adk pytest packages/kagent-adk/tests/unittests/test_local_session_store.py`.
