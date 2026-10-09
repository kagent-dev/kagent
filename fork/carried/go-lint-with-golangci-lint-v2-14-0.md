| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `build(go): lint with golangci-lint v2.14.0 for Go 1.27.2 export data` | golangci-lint v2.13.0 (x/tools v0.49.0) built with Go 1.27.2 fails every package's typecheck with `export data version 5 is greater than maximum supported version 4`; v2.14.0 (x/tools v0.50.0) reads it. The kube-api-linter plugin is built from the same golangci-lint source tag | to file with the Go bump above; dropped with it |
