| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `ci(fork): publish the charts without their ghcr.io dependencies` | gsoci is the only registry the platform pulls from: upstream's chart templates declare kmcp, kmcp-crds and kagent-tools from ghcr.io, which the platform does not run, so the published charts drop those dependencies and switch their conditions off, and `helm dependency build` of a published chart reaches no ghcr.io | fork-only |
