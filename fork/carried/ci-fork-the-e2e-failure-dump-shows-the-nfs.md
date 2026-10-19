---
id: ci-fork-the-e2e-failure-dump-shows-the-nfs
fork: kagent
theme: ci-publish-infra
category: infra
status: carried
upstream:
  outcome: fork-only
  refs: []
  exit: keep
depends_on: []
commit: 11eabfa0c
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `ci(fork): the e2e failure dump shows the NFS server and its CSI driver` | the gVisor lane's NFS server (the runner's kernel nfsd behind `csi-nfs-sc`) stopped answering for over six minutes while its pod stayed Running (giantswarm/kagent-upstream#635), and the failure dump held nothing about it. The dump now shows nfsd's state and the runner's kernel NFS messages from the artifact's `nfs/`, which holds the claims and volumes and the full logs of the NFS server, the CSI controller and node plugin | fork-only: the read-write-many NFS storage is the fork's workspace lane |
