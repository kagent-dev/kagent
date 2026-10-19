---
id: harness-claude-in-its-session-directory-changed
fork: kagent
theme: workspace-volumes
category: feature
status: carried
upstream:
  outcome: open
  refs: [kagent-dev/kagent#3157, kagent-dev/kagent#3158]
  exit: '#3157 rebased and merged'
depends_on: [harness-agent-git-trusts-the-mirrors]
commit: 8d8bf9ce1
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `feat(harness): claude in its session directory, changed repos fetched` | the `claude` Harness ran Claude Code in the fixed `/data/workspace`, so a Session admitted on a workspace volume never worked in its own directory, and that directory started empty. Claude Code is now spawned per turn in the directory the dispatch names (`x-kagent-working-directory`). Every dispatch of a Session whose source mounts the mirrors also names them (`x-kagent-mirrors-directory`) and the repositories its admission named as changed since the sync (`x-kagent-changed-repositories`). Before the first turn in such a directory the runtime clones every bare mirror with alternates (`git clone --shared --no-checkout`, `origin` the mirror's origin without user information, no working tree until the agent's `git switch`, network-filesystem settings), fetches exactly the changed repositories from origin inside the dispatched turn, under a per-repository timeout and a total budget, writes `CLAUDE.md` notes and records the setup in `.kagent/workspace.json` so no later or resumed turn sets up or fetches again. A clone or fetch that fails is the turn's first status message (a new `runtime.Notice`, a working status of the A2A task) and the turn runs. The setup is the shared `go/harness/workspace` for the Go ADK runtime to reuse; a harness that runs as root runs it as the agent's user through its own `prepare-workspace` subcommand, since inside the sandbox root may not write the user's directory on the volume | open upstream as kagent-dev/kagent#3157 (branch `upstream/claude-session-directory`, off upstream `main`). The upstream form declares the dispatch headers in `go/api/a2a` beside `DispatchHeader`, folds in the mirrors' `safe.directory` fix (`harness-agent-git-trusts-the-mirrors.md`) and leaves out the run-as-root `prepare-workspace` subcommand: upstream's image starts Claude Code as its own user, so its setup needs no hand-over. At the re-pin onto an upstream with #3157 this commit shrinks to that subcommand. #3157 conflicts with upstream `main`; its rebased head is prepared as branch `upstream/claude-session-directory-2` (`e6c7072d9`, on the mirror `main` @ `b42a6c53a`) Rebased onto upstream `main` @ `7a0a4fd0` and folded with the Go ADK setup and the returning-session notice into one upstream-ready commit as branch `upstream/session-first-turn-fetch` (`12de768a`, DCO-signed, no fork references, `make -C go test` and lint green there); it supersedes the heads above, and a human decides whether it replaces kagent-dev/kagent#3157 and #3158. |
