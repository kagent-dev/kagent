| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `ci(fork): a tag is a release of the line, vX.Y.Z-gs.N` | the line's first release scheme: a tag `vX.Y.Z-gs.N` on the consumed branch, X.Y.Z the upstream version the pin anticipated and N the line's counter, so that a release sorted above the dev builds of the same base; replaced by the decoupled `vX.Y.Z` scheme of `ci(fork): publish through the orb's stock jobs with decoupled versions` ("Release scheme") — the commit stays in the stack | fork-only, never upstream |
