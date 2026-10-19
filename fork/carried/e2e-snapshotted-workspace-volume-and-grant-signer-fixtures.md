---
id: e2e-snapshotted-workspace-volume-and-grant-signer-fixtures
fork: kagent
theme: test-flakes
category: test
status: carried
commits:
  - 'chore(e2e): snapshotted workspace volume and grant signer fixtures'
upstream:
  outcome: to-file
  refs: []
  exit: 'with #38476 row 209'
depends_on: [session-the-sessions-actor-with-its-workspace-directory]
verified: 2026-10-19
---
# chore(e2e): snapshotted workspace volume and grant signer fixtures

## Change

`chore(e2e): snapshotted workspace volume and grant signer fixtures`

## Reason

a Session started in a clone of a snapshotted volume needs, in the e2e lane, a CSI driver with snapshots, a volume holding git clones, its snapshot handle and an issuer of the signed grant the Session is admitted with (giantswarm/kagent-upstream#293). The Kind lane installs the external snapshotter and the CSI hostpath driver (`scripts/kind/setup-csi-snapshots.sh`); `newWorkspaceFixture` clones two repositories of the git fixture into a volume, snapshots it and reads the driver and handle, then moves one origin; `go/core/test/grant` signs ES256 grants and serves their JWKS; `requireNoTokenInSandbox` and `requireNoOriginRequest` are the assertions the Session workspace tests share. Test code and lane setup only

## History

to file with the Session volume source series it serves
