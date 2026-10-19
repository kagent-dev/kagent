---
id: e2e-wait-out-a-workspace-sessions-mount
fork: kagent
theme: test-flakes
category: test
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: with the upstream branch
depends_on: [session-the-sessions-actor-with-its-workspace-directory]
commit: 381de1417
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `chore(e2e): wait out a workspace Session's mount, report a Pending claim` | `TestSessionWorkspaceDirectory` failed on a claim of `csi-nfs-sc` left Pending with a bare phase comparison, and `TestClaudeSessionWorkspace` gave up on a refused turn after 30 s while its Actor's first resume was mounting the NFS volume (giantswarm/kagent-upstream#635). An unbound claim now fails after its three minutes naming its phase, its events oldest first (the provisioner's mount error among them) and the NFS server pod's state; a Session on the workspace volume resends a refused turn for three minutes, room for Substrate's retry after a 110 s mount timeout. Its unit test shows the report | to file with the Session volume source series it serves |
