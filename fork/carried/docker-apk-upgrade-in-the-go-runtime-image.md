| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(docker): apk upgrade in the Go runtime image` | the same Alpine finding in the controller and Go ADK images (`go/Dockerfile`), which #2756 did not cover | to file (upstream's `go/Dockerfile` still has no `apk upgrade`; kagent-dev/kagent#2951 pins packages in `ui/Dockerfile` only) |
