---
id: session-check-a-shares-expiry-against-the-clock
fork: kagent
theme: identity-authorizer
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: [giantswarm/kagent-upstream#124, kagent-dev/kagent#2987]
  exit: file
depends_on: []
commit: d8d582ad7
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(session): check a share's expiry against the clock that set it` | the controller sets a share's `expires_at` from its own clock and the token lookup compared it with Postgres `now()`, so skew between the two could move a share's expiry; the lookup compares with the controller's time | to file against kagent-dev/kagent#2987 (`4dce53de`), the review fix of #124's Session port |
