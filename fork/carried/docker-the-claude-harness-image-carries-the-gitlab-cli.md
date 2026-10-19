---
id: docker-the-claude-harness-image-carries-the-gitlab-cli
fork: kagent
theme: harness-variety
category: gs-specific
status: carried
commits:
  - 'feat(docker): the Claude harness image carries the GitLab CLI'
upstream:
  outcome: fork-only
  refs: [kagent-dev/kagent#3190]
  exit: tool images (#3192) replace baked CLIs
depends_on: [kagent-dev/kagent#3192]
verified: 2026-10-19
---
# feat(docker): the Claude harness image carries the GitLab CLI

## Change

`feat(docker): the Claude harness image carries the GitLab CLI`

## Reason

a coding agent writes to its forge with git and the forge's CLI from its workspace, and the Claude harness image carried `gh` but no GitLab CLI, so an agent working against GitLab could not open or read a merge request. The image installs Alpine's `glab` (1.100.0 in `alpine:3.24`, x86_64 and aarch64) at the one version in `go/gitlab-cli.version`, bind-mounted into the `apk add` step like `github-cli.version`; Renovate tracks the pin through a second `custom.regex` manager in `renovate.json`. No credential is configured. The Go runtime image does not carry it yet: its user has `HOME=/`, and `glab` creates its config directory on every command (giantswarm/kagent-upstream#429); the Claude harness hands Claude's user a writable `HOME`. `glab --version` asks gitlab.com for a newer release and, behind an egress gateway that refuses it, prints an error on stderr and exits 0

## History

fork-only, never upstream
