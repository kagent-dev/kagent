---
id: session-check-a-shares-expiry-against-the-clock
fork: kagent
theme: identity-authorizer
category: bugfix
status: carried
commits:
  - 'fix(session): check a share''s expiry against the clock that set it'
upstream:
  outcome: to-file
  refs: [giantswarm/kagent-upstream#124, kagent-dev/kagent#2987]
  exit: file
depends_on: []
verified: 2026-10-20
---
# fix(session): check a share's expiry against the clock that set it

## Change

`fix(session): check a share's expiry against the clock that set it`

## Reason

the controller sets a share's `expires_at` from its own clock and the token lookup compared it with Postgres `now()`, so skew between the two could move a share's expiry; the lookup compares with the controller's time

## History

to file against kagent-dev/kagent#2987 (`4dce53de`), the review fix of #124's Session port

kagent-dev/kagent#2987 merged 2026-10-02 (`4dce53de`, in the pin) is the Session port this fix is filed against, so it stays in `refs` as related work, giantswarm/kagent-upstream#124 as the change's own history, and the outcome stays to-file.
