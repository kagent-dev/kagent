---
id: auth-admit-one-serviceaccount-to-read-any
fork: kagent
theme: identity-authorizer
category: gs-specific
status: carried
commits:
  - 'feat(auth): admit one ServiceAccount to read any Session by ID'
upstream:
  outcome: fork-only
  refs: [giantswarm/kagent-upstream#735]
  exit: leaves with the ServiceAccount token review row if upstream takes it
depends_on: [auth-accept-a-serviceaccount-token-and]
verified: 2026-10-19
---
# feat(auth): admit one ServiceAccount to read any Session by ID

## Change

`feat(auth): admit one ServiceAccount to read any Session by ID`

## Reason

a platform service that receives a Session ID (the egress refusal notifier, giantswarm/klaus-gateway) maps the Session to its Agent, but `SessionService/GetSession` reads a Session only for its creator, a person, unless the call is the controller's own, so a ServiceAccount's read is always `NotFound` (giantswarm/kagent-upstream#735). `KAGENT_AUTHORIZATION_SESSION_READER_SERVICE_ACCOUNT` (chart `controller.authorization.sessionReaderServiceAccount`, `<namespace>/<name>`, empty by default, which admits none) names the session reader: a call the API server authenticates as that ServiceAccount by TokenReview (`fork/carried/auth-accept-a-serviceaccount-token-and.md`) is admitted to `SessionService/GetSession`, reads any Session by ID, and is refused on every other gRPC method (`PermissionDenied`), on `/mcp` and on the A2A HTTP endpoint (`401`), whatever its RBAC; a share never widens its read. A person's token or an agent's call that names the same user, every other ServiceAccount and the noop mode are unchanged; the controller refuses to start with a reader outside `trusted-proxy` authentication and `kubernetes` authorization. Docs: `docs/architecture/oidc-proxy-authentication.md`

## History

fork-only, never upstream on its own: it serves a Giant Swarm component, and it rests on the ServiceAccount token review and the Kubernetes authorizer, which upstream has not taken
