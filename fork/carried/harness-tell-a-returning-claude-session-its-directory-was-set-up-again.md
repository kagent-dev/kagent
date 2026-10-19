---
id: harness-tell-a-returning-claude-session-its-directory-was-set-up-again
fork: kagent
theme: workspace-volumes
category: feature
status: carried
commits:
  - 'feat(harness): tell a returning claude Session its directory was set up again'
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3157, kagent-dev/kagent#3158]
  exit: 'after #3158'
depends_on: [adk-set-up-the-session-directory]
verified: 2026-10-19
---
# feat(harness): tell a returning claude Session its directory was set up again

## Change

`feat(harness): tell a returning claude Session its directory was set up again`

## Reason

a Session that returns after its session directory was cleaned up gets a fresh directory from the workspace's mirrors. The Go ADK runtime told such a Session in its turn's first status message; the `claude` Harness set the directory up silently, so a returning `claude` Session could assume files it wrote before still exist. The notice and the composition of a turn's setup status message now live in `go/harness/workspace` (`FreshDirectoryNotice`, `Report.TurnNotice`) for both runtimes; the `claude` Harness counts a turn that continues an earlier conversation (a continuation ID) as returning

## History

to file — prepared in the fork: branch `upstream/claude-fresh-directory-notice`, stacked on `upstream/adk-session-directory` (kagent-dev/kagent#3158). The upstream form names the dispatch headers from `go/api/a2a`, as #3157 declares them. Rebased onto the mirror `main` @ `b42a6c53a` as branch `upstream/claude-fresh-directory-notice-2` (`80c3ffee1`, on `upstream/adk-session-directory-2`) Rebased onto upstream `main` @ `7a0a4fd0` and folded with the Go ADK setup and the returning-session notice into one upstream-ready commit as branch `upstream/session-first-turn-fetch` (`12de768a`, DCO-signed, no fork references, `make -C go test` and lint green there); it supersedes the heads above, and a human decides whether it replaces kagent-dev/kagent#3157 and #3158.
