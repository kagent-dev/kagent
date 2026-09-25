# Upstream schemas

`ateapi.proto` is an unmodified copy of
`pkg/proto/ateapipb/ateapi.proto` from `github.com/kagent-dev/substrate` at
`v0.2.0-beta5`, matching the replacement in `go/go.mod`. Update them together.
Go uses the upstream generated package; Buf generates the imported TypeScript
types for the UI. Do not edit the vendored schema.
