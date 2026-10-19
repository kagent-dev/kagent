---
id: ui-name-a-dex-user-by-the
fork: kagent
theme: identity-authorizer
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: file
depends_on: []
commit: 74546e550
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(ui): name a Dex user by the email, not the encoded subject` | behind oauth2-proxy with a Dex issuer, the userinfo document has no username or name and its `user` is Dex's encoded subject, a base64 protobuf of the user id and connector id; `toAuthUser` read `user` before `email`, so the header and the sign-in card named the person by that opaque string. The display name now takes `email` before `user`, which stays the last fallback | to file, `upstream/ui-dex-display-email` |
