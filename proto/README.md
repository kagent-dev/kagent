# Upstream schemas

`ateapi.proto` is an unmodified copy of
`pkg/proto/ateapipb/ateapi.proto` from `github.com/kagent-dev/substrate` at
`v0.4.0-alpha1`, matching the replacement in `go/go.mod`. Update them together.
Go uses the upstream generated package; Buf generates the imported TypeScript
types for the UI. Do not edit the vendored schema. Upstream alpha API changes
are excluded from kagent's protobuf compatibility check; review them with the
matching dependency upgrade.

`ateenv/v1alpha/guest.proto` is an unmodified copy from
`github.com/agent-substrate/env` at `v0.0.11-0.20260911201957-ab40c7bfb204`,
matching `go/go.mod`. Go uses the upstream generated package; kagent's sandbox
envelopes add identity, authorization, and validation at its public boundary.
