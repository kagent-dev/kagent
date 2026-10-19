---
id: ci-pin-giantswarm-architect-orb-10-10-0
fork: kagent
theme: version-pins
category: pin
status: carried
upstream:
  outcome: fork-only
  refs: []
  exit: orb release
depends_on: []
commit: f7041471f
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `ci: pin giantswarm/architect orb 10.10.0` | the hand-written CircleCI config pins the orb release that carries giantswarm/architect-orb#952 and #956; the line has `gen.ci.generate: false`, so align-files does not carry the orb version here | fork-only |
