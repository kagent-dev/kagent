# Agent identity lookup

Downstream services can resolve the Agent behind a Substrate Actor JWT through
`kagent.api.v1.AgentIdentityService/ResolveAgentIdentity`.

Send the Substrate Actor JWT in the `authorization` metadata as `Bearer <token>`.
It must be issued by the controller's configured issuer, with `kagent-agent-info`
in its audiences. A token may also target the downstream service receiving it.
Tokens containing `kagent-runtime` are rejected by this lookup. This is the only
credential required; the RPC request is empty.

```text
authorization: Bearer <Substrate Actor JWT>

{}
```

Substrate's Actor JWT source accepts explicit audiences. To let a downstream
service validate its own audience and call this lookup, configure both:

```yaml
spec:
  actorJwt:
    audiences: ["https://mcp.example.com", "kagent-agent-info"]
    expirationSeconds: 900
```

The response contains the current Agent's namespace, name, UID, labels, and
annotations, alongside the bound Session ID and its pinned runtime revision:

```json
{
  "agent": {
    "namespace": "support",
    "name": "ticket-assistant",
    "uid": "<Kubernetes Agent UID>",
    "labels": {"team": "support"},
    "annotations": {"description": "Ticket triage"}
  },
  "sessionId": "<Session UUID>",
  "runtimeRevision": "<Runtime revision digest>"
}
```

Labels and annotations are returned unchanged. They are operator-supplied
metadata and may themselves contain copied configuration, including Kubernetes
last-applied configuration annotations. The response has no spec, status, or
generic Kubernetes resource field.

The service checks the signed Actor identity against the Session's current Actor
UID and atespace, then obtains the Agent identity from the Session's saved runtime
revision. The current Kubernetes Agent must have the same UID. Obsolete actors,
deleted Sessions, and deleted or recreated Agents cannot resolve metadata. Label
and annotation edits on the same Agent UID are visible immediately.

`sessionId` and `runtimeRevision` come from that verified Session binding. The
revision stays pinned even when the Agent's current configuration changes; live
labels and annotations do not describe a snapshot of that revision. Returning
the Session ID does not grant access to the Session API or its conversation.

The shared authentication interceptor selects the metadata verifier only for
this RPC. It requires exactly one Bearer credential and passes the verified
Actor identity to the service without creating a caller principal or runtime
binding. User credentials, runtime credentials, and Session shares do not grant
lookup access. The usual interceptors still validate the request and map errors.
Signature verification shares the configured issuer's signing-key cache with
runtime authentication, but uses a separate required audience. Passing the same
metadata token to Session, runtime storage, Agent CRUD, or other APIs is rejected.

The token is bounded to 128 KiB, never persisted, and excluded from request logs
and returned errors. Possession of the token permits lookup of its own Agent
metadata until token expiry or binding revocation; the request cannot select a
different Agent.
