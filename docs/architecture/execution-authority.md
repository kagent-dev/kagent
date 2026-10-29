# Execution authority contract proposal

[`execution.proto`](../../proto/kagent/api/v1/execution.proto) defines the proposed
control-plane contract for trusted task relationships and delegated authority.
The protobuf and generated clients support contract review ahead of service
registration and persistence changes.

## Ownership

A2A owns task state, messages, artifacts, subscriptions, and cancellation.
`ExecutionService` exposes the controller's immutable relationships between those
tasks. A task reference contains its Session and A2A task identifiers. Parent,
root, continuation, and background relationships originate from authenticated
dispatch and are persisted atomically with task admission. Native cancellation
uses those relationships.

Session authority separates the initiating principal, storage owner, and
Substrate Actor. `SessionAuthority.scope_ceiling` carries the immutable upper
bound. The effective scope set is the intersection of the initiator's current
scopes and every inherited ceiling. A present empty ceiling represents
an empty grant; an absent ceiling inherits its parent. Parent revocation and
expiry apply throughout the delegation tree.

## Credential surfaces

`GetRuntimeAuthority` resolves a verified, live Actor JWT to its Session authority.
The runtime reuses the existing authentication provider, authorizer, collection
authorizer, and token source hooks.

Delegation registration verifies a refresh credential against the configured IdP
and binds it to the authenticated principal. The native token source owns
encrypted durable storage, coordinated refresh, revocation, and current-scope
checks. Status responses expose renewability; credential contents stay private.

Session credentials carry an explicit Session binding and a maximum one-hour
lifetime for channel and voice integrations. The issuing service applies caller
authorization and inherited expiry before signing. Each use checks the current
Session authority, including revocation.
