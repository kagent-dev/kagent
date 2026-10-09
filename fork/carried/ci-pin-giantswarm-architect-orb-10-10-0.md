| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `ci: pin giantswarm/architect orb 10.10.0` | the hand-written CircleCI config pins the orb release that carries giantswarm/architect-orb#952 and #956; the line has `gen.ci.generate: false`, so align-files does not carry the orb version here | fork-only |
