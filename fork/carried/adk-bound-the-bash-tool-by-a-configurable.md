---
id: adk-bound-the-bash-tool-by-a-configurable
fork: kagent
theme: harness-variety
category: feature
status: carried
upstream:
  outcome: open
  refs: [giantswarm/kagent-upstream#405, kagent-dev/kagent#2357, kagent-dev/kagent#3006, kagent-dev/kagent#3131, giantswarm/kagent-upstream#400]
  exit: '#3131 merges; env var to be moved into config schema (#3153)'
depends_on: [adk-enforce-the-bash-tool-timeout-when]
commit: a86f65aad
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(adk): bound the bash tool by a configurable timeout and the turn`, `fix(adk): reap the background jobs a turn's end kills` and `chore(adk): wait for the killed process group in the timeout test` | a coding agent's `git clone`, `fetch` or `push` of a real repository outlasts the fixed 30 s limit (60 s when the command mentions python), and a process one turn left in the background kept running into the next turn, which in a shared Session runs for another caller with that caller's credentials. The limit is now `KAGENT_BASH_TOOL_TIMEOUT` on the runtime (a Go duration, default 10m; the python rule is gone) and the tool's description tells the model the limit; the A2A executor records every process group a turn's commands started and kills what is still running when the turn ends, before the event that ends it is yielded, and reaps the killed processes it is the parent of: in a sandbox the runtime is PID 1, an orphaned background job is reparented to it, and unreaped it stayed a zombie that `pgrep` counts. Proven by unit tests and by the fork e2e on the `kagent` Harness (`TestBashToolEndsBackgroundJobsWithTheTurn`, `TestBashToolTimeoutFitsGit` against the git host fixture with a throttled path). The timeout test waits for the killed group (2 s, under the 6 s an unkilled `sleep 8` would still run) instead of checking it the instant the call returns: a timeout kills the group before the call returns, the killed processes exit when the scheduler next runs them, and on a saturated runner the single check still found `sleep` running (giantswarm/kagent-upstream#387; 16 of 60 runs in a PID namespace on two saturated cores, none with the wait) | kagent-dev/kagent#3131 (open): the turn end and the reap, on top of kagent-dev/kagent#3006; the configurable timeout stays fork-only until kagent-dev/kagent#2357 settles its configuration surface. The test's wait is prepared in the fork for #3131: branch [`upstream/adk-bash-turn-bound`](https://github.com/giantswarm/kagent-upstream/tree/upstream/adk-bash-turn-bound) (`a8e85156`) — #3131's three commits rebased onto upstream `main` @ `92dd6caa` (the mirror's pin) with the fork's `f481f1ce` on top (giantswarm/kagent-upstream#405; authored as `b460d53e` on #400, which fell behind the base), as `test(adk): wait for a killed process group through one helper`, DCO-signed, no fork references, `go test -race ./go/adk/pkg/tools` green. On #3131's code the fork's `TestExecuteCommand_TimeoutBoundsTheCommand` does not exist (it came with the configurable timeout) and #3006's `TestExecuteCommand_TimeoutKillsChildProcesses` already waits for its child, so the upstream commit is the shared `groupExits` helper, the turn-end test on it and the contract stated on `ExecuteCommand`. #3131 takes the branch whenever its author chooses |
