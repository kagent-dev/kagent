# Upstream schemas

`api/ateapi/v1/ateapi.proto` is an unmodified copy of
`api/ateapi/v1/ateapi.proto` from `github.com/kagent-dev/substrate` at
`v1.0.0-rc1`, matching the replacement in `go/go.mod`. Update them together.
Go uses the upstream generated package; Buf generates the imported TypeScript
types for the UI. Do not edit the vendored schema. Upstream API changes are
included in kagent's protobuf compatibility check and must be reviewed with the
matching dependency upgrade. The v1.0.0-rc1 release renames
`SnapshotStorage.status` to `state` and `SnapshotStorageStatus` to
`SnapshotStorageState`, and removes the unused `MintActorCertificate` RPC and
messages. The snapshot field number and enum values are unchanged, but JSON
clients must use the new names. Upgrade Kagent clients and the controller together.

`ateenv/v1alpha/guest.proto` is an unmodified copy from
`github.com/agent-substrate/env` at `v0.0.11-0.20261007184417-0d359ea73823`,
matching `go/go.mod`. Go uses the upstream generated package; kagent's sandbox
envelopes add identity, authorization, and validation at its public boundary.
