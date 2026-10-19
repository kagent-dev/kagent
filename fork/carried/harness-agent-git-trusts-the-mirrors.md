---
id: harness-agent-git-trusts-the-mirrors
fork: kagent
theme: workspace-volumes
category: bugfix
status: carried
commits:
  - 'fix(harness): let Claude''s own git trust the session''s mirrors'
upstream:
  outcome: open
  refs: [kagent-dev/kagent#3157, kagent-dev/kagent#3158]
  exit: '#3157 merges'
depends_on: [harness-claude-in-its-session-directory-changed]
verified: 2026-10-19
---
# fix(harness): let Claude's own git trust the session's mirrors

## Change

`fix(harness): let Claude's own git trust the session's mirrors`

## Reason

The bare mirrors on a Session's read-only `/mirrors` mount belong to the volume's owner, and the `claude` Harness starts Claude Code as uid 65532. The setup clone trusted a mirror on its own command line only (`-c safe.directory=<mirror>`), so the agent's own `git clone /mirrors/<owner>/<repo>.git` failed on git's dubious-ownership check and any repository the setup had not cloned stayed unreachable. Claude Code's environment now carries `safe.directory=<mirrors>/*` through `GIT_CONFIG_COUNT` when the dispatch names a mirrors directory: no file is written, an entry already configured is kept, and nothing outside the mirrors is trusted. [#409](https://github.com/giantswarm/kagent-upstream/issues/409)

## History

folded into kagent-dev/kagent#3157 (branch `upstream/claude-session-directory`) with the commit it fixes; drops at the re-pin onto an upstream with #3157 Rebased onto upstream `main` @ `7a0a4fd0` and folded with the Go ADK setup and the returning-session notice into one upstream-ready commit as branch `upstream/session-first-turn-fetch` (`12de768a`, DCO-signed, no fork references, `make -C go test` and lint green there); it supersedes the heads above, and a human decides whether it replaces kagent-dev/kagent#3157 and #3158.
