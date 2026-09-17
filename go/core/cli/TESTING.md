# CLI Testing Guide

CLI tests live beside the code they cover. Prefer focused unit tests with fakes
defined in the test file; add shared helpers only after multiple tests need the
same behavior.

Run the CLI suite from the repository root:

```bash
go test ./go/core/cli/...
```

Useful variants:

```bash
go test -race ./go/core/cli/...
go test -cover ./go/core/cli/...
go test -tags=integration ./go/core/cli/...
```

The integration tag currently covers MCP project generation and requires its
external tools. Unit tests must not require a cluster, Docker, or network access.

For TUI behavior, construct the model directly, send Bubble Tea messages through
`Update`, and assert the resulting model or rendered view. Keep terminal and
network dependencies behind the existing command and connection boundaries.
