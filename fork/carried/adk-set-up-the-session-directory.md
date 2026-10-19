---
id: adk-set-up-the-session-directory
fork: kagent
theme: workspace-volumes
category: feature
status: carried
upstream:
  outcome: open
  refs: [kagent-dev/kagent#3131, kagent-dev/kagent#3157, kagent-dev/kagent#3158]
  exit: '#3158 rebased and merged'
depends_on: [adk-register-the-execution-tools-without-skills, harness-claude-in-its-session-directory-changed]
commit: 3a0eee160
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `feat(adk): set up the session directory, fetch changed repos` | on the `kagent` Harness a Session admitted on a workspace volume started with an empty directory and the Go ADK's tools never reached it: `bash` ran in the scratch directory and the file tools resolved paths there. The Go ADK now reads the turn's dispatch per turn (`x-kagent-working-directory`, `x-kagent-mirrors-directory`, `x-kagent-changed-repositories`): before the first turn in a directory with mirrors it sets the directory up with the shared `go/harness/workspace` (moved out of `go/harness/internal` for it), a clone with alternates of every mirror (`git clone --shared --no-checkout`, `origin` the provider's URL, checked out at the agent's first `git switch`), then fetches exactly the changed repositories inside the dispatched turn, under a per-repository timeout and a total budget, so the fetch leaves through the egress gateway as the turn's caller with no token in the sandbox, in the environment `bash` gets (`turn.CommandEnvironment`, moved from the tools). `AGENTS.md` notes say how to start work in a repository, and the working-directory rule names them; `.kagent/workspace.json` makes the setup once, a resumed turn (an answer or an approval) sets up nothing, and a session with earlier turns whose directory is set up again is told so. A clone or fetch that fails is the message of the turn's first working status, naming the repository and the reason, and the turn runs. `bash` runs in the turn's directory and the file tools resolve relative paths there, the scratch directory staying reachable by its absolute path; a turn without a directory keeps the scratch directory. Proven by unit tests on the executor (no volume, a refused fetch, a resumed turn, a returning session) and the tools, and by the fork e2e `TestGoADKSessionWorkspace` on a Declarative agent with the bash tool | open upstream as kagent-dev/kagent#3158 (branch `upstream/adk-session-directory`, stacked on #3157). The upstream form builds on upstream's skills-only `NewSkillExecutionTools` rather than on `adk-register-the-execution-tools-without-skills.md`, and keeps the turn's directory in a `go/adk/pkg/turn` package (`directory.go`), beside kagent-dev/kagent#3131's files in that package; the tools resolve paths in the turn's directory exactly as here. #3158 conflicts with upstream `main`; its rebased head is prepared as branch `upstream/adk-session-directory-2` (`f01184e29`, on `upstream/claude-session-directory-2`) Rebased onto upstream `main` @ `7a0a4fd0` and folded with the Go ADK setup and the returning-session notice into one upstream-ready commit as branch `upstream/session-first-turn-fetch` (`12de768a`, DCO-signed, no fork references, `make -C go test` and lint green there), queued as review-list row 211; it supersedes the heads above, and a human decides whether it replaces kagent-dev/kagent#3157 and #3158. |
