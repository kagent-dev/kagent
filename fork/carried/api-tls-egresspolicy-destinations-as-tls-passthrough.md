---
id: api-tls-egresspolicy-destinations-as-tls-passthrough
fork: kagent
theme: egress-policies
category: feature
status: carried
commits:
  - 'feat(api): tls:// EgressPolicy destinations as TLS passthrough'
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3177, giantswarm/kagent-upstream#709]
  exit: upstream takes the tls:// kind and a re-pin brings it; the line then drops this port
depends_on: [api-add-reusable-egress-policies-for-agents]
verified: 2026-10-19
---
# feat(api): tls:// EgressPolicy destinations as TLS passthrough

## Change

`feat(api): tls:// EgressPolicy destinations as TLS passthrough`

## Reason

an agent that needs a TLS service that is not HTTP, such as a database or a message broker, could not reach it: an `EgressPolicy` origin was HTTP(S) only, although Substrate has a TLS passthrough rule matched on the SNI server name and a port set. An `EgressPolicy` destination now also accepts `tls://<host>:<port>`: the host rules of an HTTP(S) origin (lowercase DNS name, a leftmost-label wildcard over at least two labels, no IP address), a required port, no authentication. The CRD pattern and CEL rules, `egress.ParseOrigin`, `egress.CanonicalAccess` and `substrate.ActorEgressPolicy` share the one origin grammar; the compiler emits one `TLSPassthroughRule` per entry, with no effects; HTTP, HTTPS and TLS passthrough cannot share a hostname pattern and port; a credential binds only to a hostname with an HTTPS origin; a model, MCP server or artifact URL never infers a passthrough grant (`egress.Origin` yields an opaque origin for any other scheme). Ported onto the line's `EgressPolicy` carry, whose bindings stay keyed by hostname. Persisted state keeps its format: a revision's destinations are origin strings, and a `tls://` entry is one more of them; a controller older than this change refuses to prepare a revision that holds one. `Agent.spec.egress` takes the same `tls://` grammar (`api-tls-agent-spec-egress-destinations-as-tls-passthrough.md`). The re-pin onto upstream `main` at `7a0a4fd0` (giantswarm/kagent-upstream#709) takes upstream's `EgressPolicy`, keyed by origin: it re-keys this port onto that keying, which is the form of the prepared upstream branch, and drops the hostname-keyed parts. `TestTLSPassthroughOrigins`, `TestActorPolicyCompilesTLSPassthrough`, `TestEgressPolicyTLSPassthrough`, the `TLS auth` case of `TestEgressPolicyResolutionFailsClosed`, the `TLS` cases of the EgressPolicy CEL tests

## History

to file — prepared in the fork: branch `upstream/egress-policy-tls-passthrough-v2` (one DCO-signed commit on upstream `main` at `7a0a4fd0`, which contains kagent-dev/kagent#3177; no fork references; its message the pull request text). kagent-dev/kagent searched 2026-10-19: nothing proposes a TLS passthrough or non-HTTP kind for `EgressPolicy`.
