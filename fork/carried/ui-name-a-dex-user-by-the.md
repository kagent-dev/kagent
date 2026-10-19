---
id: ui-name-a-dex-user-by-the
fork: kagent
theme: identity-authorizer
category: bugfix
status: carried
commits:
  - 'fix(ui): name a Dex user by the email, not the encoded subject'
upstream:
  outcome: to-file
  refs: []
  exit: file
depends_on: []
verified: 2026-10-19
---
# fix(ui): name a Dex user by the email, not the encoded subject

## Change

`fix(ui): name a Dex user by the email, not the encoded subject`

## Reason

behind oauth2-proxy with a Dex issuer, the userinfo document has no username or name and its `user` is Dex's encoded subject, a base64 protobuf of the user id and connector id; `toAuthUser` read `user` before `email`, so the header and the sign-in card named the person by that opaque string. The display name now takes `email` before `user`, which stays the last fallback

## History

to file, `upstream/ui-dex-display-email`
