# Upstream schemas

`api/ateapi/v1/ateapi.proto` is an unmodified copy of
`api/ateapi/v1/ateapi.proto` from `github.com/kagent-dev/substrate` at
`v0.7.0-alpha2`, matching the replacement in `go/go.mod`. Update them together.
Go uses the upstream generated package; Buf generates the imported TypeScript
types for the UI. Do not edit the vendored schema. Upstream API changes
are excluded from kagent's protobuf compatibility check; review them with the
matching dependency upgrade. The v0.7.0-alpha1 release relocates the schema and
renames its package to `substrate.ateapi.v1` without changing message fields or
enum values. The inventory fields in `system.proto` follow those new type names;
their temporary `FIELD_SAME_TYPE` exception can be removed after the upgrade
reaches the compatibility comparison branch.

`ateenv/v1alpha/guest.proto` is an unmodified copy from
`github.com/agent-substrate/env` at `v0.0.11-0.20261007184417-0d359ea73823`,
matching `go/go.mod`. Go uses the upstream generated package; kagent's sandbox
envelopes add identity, authorization, and validation at its public boundary.
