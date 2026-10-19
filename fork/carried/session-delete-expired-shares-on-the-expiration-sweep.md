---
id: session-delete-expired-shares-on-the-expiration-sweep
fork: kagent
theme: identity-authorizer
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#2987]
  exit: file
depends_on: []
commit: f87f42bb8
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(session): delete expired shares on the expiration sweep` | an expired share stops granting access at its `expires_at`, but its row stayed in `session_share` until the owner revoked it or the session was deleted; the platform's Slack gateway (giantswarm/klaus-gateway) keeps one share per thread, so every thread nobody writes to again left a dead row behind. The leader's expiration sweep deletes the shares expired at the controller's clock on its poll interval and counts them in `kagent.session.share_expired`; a zero idle TTL turns idle deletion off and no longer idles the worker | to file against kagent-dev/kagent#2987 (`4dce53de`) — prepared in the fork: branch `upstream/session-share-expiry-sweep` (mirror `main` + the patch, DCO-signed, no fork references) |
