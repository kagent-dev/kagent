| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(helm): sanitise the chart version in the app.kubernetes.io/version label` | helm-controller installs the chart as `<version>+<digest>`, which is not a valid label value | to file (kagent-dev/kagent#695 reports the same failure on Flux OCI installs, closed NOT_PLANNED 2026-05) |
