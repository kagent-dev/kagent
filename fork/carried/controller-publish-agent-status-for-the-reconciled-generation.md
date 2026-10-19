---
id: controller-publish-agent-status-for-the-reconciled-generation
fork: kagent
theme: golden-boot
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: file the branch
depends_on: []
commit: c10fd7b41
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(controller): publish Agent status for the reconciled generation` | the Agent status stamped the Agent's current generation on conditions computed from the previous generation's reconciliation, so right after a spec change `Ready=True` named the new generation while the latest successful revision was still the previous one. A client that waits for `Ready` at the generation it wrote and then creates a Session (the portal after an edit, the e2e tests) got a Session on the previous revision: after an `Agent.spec.egress` change its Actor lacked the new origins and the egress gateway answered 403 (giantswarm/kagent-upstream#671). The status now carries the generation its reconciliation compiled | to file — prepared in the fork: branch `upstream/agent-status-reconciled-generation` |
