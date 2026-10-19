---
id: harness-glab-acts-as-the-person-through-the
fork: kagent
theme: credential-injection
category: feature
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: 'with the bash tool placeholder row it extends; the placeholder value is GS-specific'
depends_on: [adk-run-bash-tool-commands-as-the-person]
commit: giantswarm/kagent-upstream#727
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `feat(harness): glab acts as the person through the placeholder like gh` | in a Session of a GitLab workspace `git push` reached GitLab as the person, but `glab` could not: without a token glab sends no credential, and with one from `GITLAB_TOKEN` it sends a `PRIVATE-TOKEN` header, neither of which the egress gateway replaces, so `glab api user` answered 401 and the Session could not open a merge request (giantswarm/kagent-upstream#723). `WithCredentialPlaceholders` now also sets `GITLAB_TOKEN` to the placeholder and `GLAB_IS_OAUTH2=true` in a turn with a working directory, so glab sends `Authorization: Bearer <placeholder>`, which the gateway replaces with the person's token on a GitLab host; a glab token the environment sets (`GITLAB_TOKEN`, `GITLAB_ACCESS_TOKEN`, `OAUTH_TOKEN`) stands, and the bash tool's allowlist keeps a runtime's real token from the command. glab also acts only on a remote whose host its configuration knows besides gitlab.com, so the workspace setup writes `.kagent/glab-cli/config.yml` (0600) naming the host of every http(s) origin but github.com as an OAuth2 GitLab host, and `GLAB_CONFIG_DIR` points at it. `TestWithCredentialPlaceholders`, `TestGlabConfig`, `TestPrepareWritesGlabConfig`, `TestExecuteCommand_TurnEnvironment` | to file with `feat(adk): run bash tool commands as the person of the turn`, whose placeholder it extends |
