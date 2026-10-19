---
id: adk-register-the-execution-tools-without-skills
fork: kagent
theme: harness-variety
category: feature
status: carried
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3158, kagent-dev/kagent#3222]
  exit: 'file after #3158'
depends_on: [adk-set-up-the-session-directory]
commit: 5026a27fc
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `feat(adk): register the execution tools without skills` | the Go ADK registered `bash`, `read_file`, `write_file` and `edit_file` only when the agent had skills, so a Declarative agent without skills, started in a Session with a workspace directory, had no tool that runs git and its workspace was unreachable however current it was. The four tools (and the opt-in `list_files` and `grep_file`) are now registered for every agent, since the runtime does not know at boot whether a Session will carry a workspace; their descriptions name the working-directory rule (the turn's directory, whose top level holds one `<owner>/<repository>` directory per repository of the workspace; `/data/workspace` in a Session without one) and the scratch directory (`uploads/`, `outputs/`) by its absolute path. The skills toolset and the scratch directory's `skills/` link stay bound to skills, and a skills hint is in the descriptions only then. The tools' working directory and path root are the session directory setup's (`adk-set-up-the-session-directory.md`). Proven by unit tests on the tool factory and on the agent's wiring, and by the fork e2e `TestSessionWorkspaceToolsWithoutSkills` on the `kagent` Harness: an agent without skills in a Session on the workspace fixture runs `git status` in a repository of its session directory through `bash`, the model is offered the four tools with the rule and no skills tool | to file: prepared off upstream `main` once kagent-dev/kagent#3158 is merged, as the follow-up that lets an agent without skills work in its session directory; kagent-dev/kagent searched 2026-10-12: upstream registers the execution tools only with skills |
