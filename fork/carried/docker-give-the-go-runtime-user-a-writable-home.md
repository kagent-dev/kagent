---
id: docker-give-the-go-runtime-user-a-writable-home
fork: kagent
theme: ci-publish-infra
category: bugfix
status: carried
upstream:
  outcome: open
  refs: [kagent-dev/kagent#3161]
  exit: '#3161 merges'
depends_on: []
commit: 501a16fc1
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(docker): give the Go runtime user a writable HOME` | the Go runtime image ran uid 65532 without a passwd entry, so `HOME` was `/`, owned by root, and the Go ADK hands the runtime's `HOME` to every command the model runs: `git config --global` and CLIs that create `$HOME/.config` (`glab`) failed with `mkdir /.config: permission denied` (giantswarm/kagent-upstream#429). The image gives uid 65532 a passwd entry with `/home/kagent`, owned by it, and sets `HOME` there, since Substrate takes `HOME` from the image's `ENV`, not from passwd | kagent-dev/kagent#3161 |
