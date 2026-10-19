---
id: substrate-send-exactly-on-commit-and-storage-location
fork: kagent
theme: test-flakes
category: test
status: carried
commits:
  - 'chore(substrate): send exactly on_commit and storage_location in snapshot_config'
upstream:
  outcome: none
  refs: [kagent-dev/kagent#2708]
  exit: keep
depends_on: []
verified: 2026-10-19
---
# chore(substrate): send exactly on_commit and storage_location in snapshot_config

## Change

`chore(substrate): send exactly on_commit and storage_location in snapshot_config`

## Reason

Substrate 1.9's ate-api refuses an ActorTemplate whose `snapshot_config` carries a field it does not know (`actor_template.snapshot_config: Invalid value: unknown field with protobuf tag 1`, Agent `Ready=False ActorTemplateRejected`): `on_pause` (tag 1) left the protos upstream (agent-substrate/substrate#2309). The line's `1.10.x` builds against Substrate 1.8 protos and sets `OnPause`; from the re-pin onto upstream `92dd6caa` (giantswarm/kagent-upstream#481, `v1.11.0-rc.13`) the line builds against Substrate `1.9.0` and its compilers (`go/core/internal/substrate/actor_template.go`, `sandbox.go`) set only `OnCommit`, as upstream does since kagent-dev/kagent#2708 (`bc3c1714c`). The commit is the line's own guard on that drift: the compiled Agent and sandbox templates are checked on the wire, no unknown field and exactly `on_commit` and `storage_location`, so a re-pin or a proto pin that sends a field the consumed ate-api lacks fails the unit tests instead of a lab (giantswarm/kagent-upstream#458)

## History

none — the change itself is upstream since kagent-dev/kagent#2708; the wire check is the line's guard on its Substrate pin; no upstream branch
