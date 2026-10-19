---
id: harness-hand-a-session-directory-without-mirrors
fork: kagent
theme: workspace-volumes
category: bugfix
status: carried
commits:
  - 'fix(harness): hand a session directory without mirrors to Claude''s user'
upstream:
  outcome: fork-only
  refs: [kagent-dev/kagent#3157]
  exit: with the run-as-root row
depends_on: [harness-run-claude-code-as-the-images-unprivileged]
verified: 2026-10-19
---
# fix(harness): hand a session directory without mirrors to Claude's user

## Change

`fix(harness): hand a session directory without mirrors to Claude's user`

## Reason

Substrate creates a missing read-write `sub_path` for the container's process user, and the `claude` Harness runs as root and starts Claude Code as uid 65532. `prepareAsUser` handed the session directory to Claude's user only when the dispatch named a mirrors directory, so on a volume source without `/mirrors` the directory stayed root:root 0770 and Claude Code could not enter it (`cd: /workspace: Permission denied`). Root now hands the dispatched directory over on every turn before the mirrors check, best effort as before. A directory that is already root-owned is fixed on the Session's next turn; no mode changes and nothing below the directory is touched. [#406](https://github.com/giantswarm/kagent-upstream/issues/406)

## History

not opened upstream: it fixes the run-as-root `prepare-workspace` step, which kagent-dev/kagent#3157 leaves out (upstream's image starts Claude Code as its own user), so upstream has nothing it applies to. It stays carried with that step
