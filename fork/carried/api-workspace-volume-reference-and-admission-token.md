---
id: api-workspace-volume-reference-and-admission-token
fork: kagent
theme: workspace-volumes
category: feature
status: carried
upstream:
  outcome: open
  refs: [agent-substrate/substrate#1637, kagent-dev/kagent#3153, kagent-dev/kagent#3155, kagent-dev/kagent#3156]
  exit: '#3155 rebased and merged'
depends_on: [giantswarm/substrate:existing-volume-mounted-per-actor-at-sub, giantswarm/substrate:read-write-mounts-missing-sub-path-directory]
commit: 46c8c313d
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `feat(api): workspace volume reference and admission token on CreateSession` | the platform starts a Session in its own directory of a workspace's shared volume: `CreateSessionRequest.volume_source` names an existing read-write-many volume (CSI driver and handle) and its mounts (a clean relative sub-path of the volume, `${SESSION_ID}` standing for the Session's id, at a clean absolute path outside `/data`, at most one read-write: the Session's own directory, the workspace's mirrors read-only), and `admission_token` carries the proof the installation's admission verifies; kagent never creates or deletes the volume. The source is stored on the Session row and shown on get and list; the token is handed to the admission and stored or shown nowhere; a retry under the same `request_id` must name the same source. A request without the fields is handled exactly as before. Until a `session.VolumeAdmission` is configured (the next patch, the `jwt` admission of the workspace grant), every request that names a source or carries a token is refused `FailedPrecondition` "this installation admits no Session volume", so no caller mounts a volume of its choosing; an `Agent` or `AgentTemplate` carries no volume, which the envtest proves against the shipped CRDs. API and storage only: the admission and the actor's mounts follow | open upstream as kagent-dev/kagent#3155 (branch `upstream/session-volume-source`, off upstream `main`): the same API, storage and refusal; the source is `Session.volume_source` (field 17) as here, stored for the actor mount that follows once Substrate can mount an existing volume (agent-substrate/substrate#1637); drops at the re-pin onto an upstream with #3155. #3155 conflicts with upstream `main` since kagent-dev/kagent#3153; its rebased head is prepared as branch `upstream/session-volume-source-3` (`258bef34f`, on the mirror `main` @ `b42a6c53a`), for its owner to move the pull request to Rebased onto upstream `main` @ `7a0a4fd0` and folded with the jwt admission into one upstream-ready commit as branch `upstream/session-volume-admission` (`d1365420`, DCO-signed, no fork references, `make -C go test` and lint green there), queued as review-list row 209; it supersedes the heads above, and a human decides whether it replaces kagent-dev/kagent#3155 and #3156. |
