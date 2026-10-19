---
id: adk-run-bash-tool-commands-as-the-person
fork: kagent
theme: harness-variety
category: gs-specific
status: carried
commits:
  - 'feat(adk): run bash tool commands as the person of the turn'
  - 'fix(harness/claude): commit as the person of the turn'
  - 'fix(harness): codex and omp commit as the person of the turn too'
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3014]
  exit: placeholder value is GS-specific; file the identity half only
depends_on: [adk-run-bash-tool-commands-on-an-allowlisted, runtime-name-the-agent-and-the-user]
verified: 2026-10-19
---
# feat(adk): run bash tool commands as the person of the turn

## Change

`feat(adk): run bash tool commands as the person of the turn`

## Reason

the allowlisted environment of the bash tool (the row "run bash tool commands on an allowlisted environment") admits neither a git identity nor `GH_TOKEN`, so a coding agent working as a person could not commit as them and the Harness images' `gh` failed every authenticated call (giantswarm/kagent-upstream#290). A command now gets git's author and committer from the person of the turn (`x-kagent-user`; a turn without one gets none, so git's own error stands) and, in a turn whose dispatch names a workspace directory, `GH_TOKEN` set to the inert placeholder `kagent-credential-injected` that the egress gateway replaces with the caller's credential; a real `GH_TOKEN` in the runtime never reaches the command

## History

to file with the allowlist row and "name the agent and the user on every model call", both of which it builds on: the allow-list keeps a placeholder where kagent-dev/kagent#3014's strip by name (`TOKEN`) would remove it
