| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `ci(fork): stamp the golang-adk digest and SUBSTRATE_VERSION into the chart at publish` | the published chart carries the index digests of its own runtime images and the Substrate worker image of the Makefile's pin, so the platform's meta chart stops re-pinning by hand what the line already knows (giantswarm/giantswarm#37765) | fork-only |
