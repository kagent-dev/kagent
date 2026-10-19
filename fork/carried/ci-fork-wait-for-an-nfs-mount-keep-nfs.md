---
id: ci-fork-wait-for-an-nfs-mount-keep-nfs
fork: kagent
theme: ci-publish-infra
category: infra
status: carried
commits:
  - 'ci(fork): wait for an NFS mount, keep NFS state in the e2e artifact'
upstream:
  outcome: fork-only
  refs: []
  exit: keep
depends_on: []
verified: 2026-10-19
---
# ci(fork): wait for an NFS mount, keep NFS state in the e2e artifact

## Change

`ci(fork): wait for an NFS mount, keep NFS state in the e2e artifact`

## Reason

the gVisor lane's NFS server pod is available before its nfsd answers (no readiness probe; nfsd waits out client tracking and a 10-second grace period), and in one run it stopped answering mid-run for minutes with nothing kept to tell why (giantswarm/kagent-upstream#639). The setup now ends only once a probe claim of `csi-nfs-sc` is mounted, written and deleted; during the tests `scripts/kind/nfs-diagnostics.sh` samples nfsd's threads, pool counters and an NFSv4 NULL call with the runner's pressure every 15 seconds, and after them writes the server's and the CSI driver's full logs, nfsd's state and the kernel messages, all into the artifact's `nfs/`

## History

fork-only: the read-write-many NFS storage is the fork's workspace lane
