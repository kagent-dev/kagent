| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(deps): bump golang.org/x/net to v0.61.0 for two HTTP/2 CVEs` | golang.org/x/net below v0.61.0 carries two HTTP/2 vulnerabilities, CVE-2026-97032 and CVE-2026-78663, which fail the nancy step of every pull request's CircleCI build; the golang.org/x modules x/net v0.61.0 requires come along | to file; giantswarm/giantswarm#37742 (upstream main is on x/net v0.59.0); dropped at the re-pin onto an upstream that has it |
