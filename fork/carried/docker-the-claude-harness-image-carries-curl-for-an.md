---
id: docker-the-claude-harness-image-carries-curl-for-an
fork: kagent
theme: harness-variety
category: feature
status: carried
commits:
  - 'feat(docker): the Claude harness image carries curl for an HTTP error''s reason'
upstream:
  outcome: to-file
  refs: []
  exit: Claude Code's WebFetch relays an error response's status text and body
depends_on: []
verified: 2026-10-19
---
# feat(docker): the Claude harness image carries curl for an HTTP error's reason

## Change

`feat(docker): the Claude harness image carries curl for an HTTP error's reason`

## Reason

Claude Code's WebFetch answers an HTTP error with its status code alone, so an agent on the Claude harness whose fetch the egress gateway refused saw `403 Forbidden` without the gateway's reason (the refused host, that it is not in the agent's egress policy, that the agent's author can allow it) and told its user the server blocks automated access. The image installs Alpine's `curl` and ships `go/harness/claude/managed-CLAUDE.md` as Claude Code's managed `/etc/claude-code/CLAUDE.md`, which every turn loads beside the harness's managed settings: an HTTP fetch whose WebFetch answer is an error is fetched again with `curl -sS -i`, and the answer names the reason from its status line and body. `go/.dockerignore` lets that one Markdown file into the build context

## History

to file — prepared in the fork: branch `upstream/claude-harness-curl`
