# Egress policies

`EgressPolicy` grants additional HTTP(S) and TLS passthrough access to an
Agent or standalone sandbox. An Agent selects policies through `spec.egressPolicyRefs`; a
SandboxTemplate selects them through the same field. References resolve in the
workload's namespace. Policies are managed through the Kubernetes API.

AgentTemplate owns reusable behavior, and Harness owns how it runs. Neither
selects egress policies. This lets two Agents reuse a template while granting
different access for their tasks. A shared child template contributes its
inferred model, MCP, and artifact destinations, but cannot select additional
policies. The root and all shared children execute in one Actor and share its
complete grant; there is no per-child network isolation.

## Grant access for a task

For example, a dependency-installation policy can grant access to registries:

```yaml
apiVersion: api.kagent.dev/v1alpha3
kind: EgressPolicy
metadata:
  name: node-dependencies
  namespace: team-a
spec:
  destinations:
    - origin: https://registry.npmjs.org
    - origin: https://nodejs.org
---
apiVersion: api.kagent.dev/v1alpha3
kind: Agent
metadata:
  name: code-reviewer
  namespace: team-a
spec:
  templateRef:
    name: code-review
  harnessRef:
    name: codex
  egressPolicyRefs:
    - name: node-dependencies
```

An origin is a scheme, DNS name, and optional port. Omitted ports mean 443 for
HTTPS and 80 for HTTP. Every path on that origin is allowed. URL paths, queries,
fragments, user information, IP addresses, and other schemes are rejected.
A wildcard replaces exactly one leftmost label: `https://*.example.com`
allows `api.example.com`, but neither `example.com` nor `a.b.example.com`.
`*` and `*.com` are not accepted. Redirects and downloads from other origins
require their own grants. A registry grant is not a guarantee that every
package's install script or release download can reach its dependencies.

## Reach a TLS service that is not HTTP

A `tls://` origin allows TLS that the gateway forwards without decryption, for
a database or message broker that speaks TLS:

```yaml
apiVersion: api.kagent.dev/v1alpha3
kind: EgressPolicy
metadata:
  name: orders-database
  namespace: team-a
spec:
  destinations:
    - origin: tls://orders-db.example.com:5432
    - origin: tls://*.kafka.example.com:9093
```

The port is required; the hostname follows the rules of an HTTP(S) origin,
wildcard included. The gateway matches the SNI server name and port of each
connection's ClientHello and reads nothing after it: a client that sends no SNI,
and a protocol that upgrades to TLS after a cleartext exchange (STARTTLS), do
not match. A passthrough destination cannot carry authentication, and its
hostname pattern and port cannot also be an HTTP or HTTPS origin, because
Substrate selects the three rule kinds by the same hostname pattern and port. A
credential binds only to a hostname with an HTTPS origin, never to one a
passthrough origin alone allows. A model, MCP server or artifact URL never
infers a passthrough grant. A policy change reaches an open passthrough
connection only when it ends.

For a standalone sandbox, select the policy on its template:

```yaml
apiVersion: api.kagent.dev/v1alpha3
kind: SandboxTemplate
metadata:
  name: node-tools
  namespace: team-a
spec:
  workload:
    image: registry.example.com/tools@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  substrate:
    workerPoolRef:
      name: default
    snapshotPolicy:
      location: s3://example/snapshots/
  egressPolicyRefs:
    - name: node-dependencies
```

`Agent.spec.egress` lists origins inline on a single Agent; an EgressPolicy is the
reusable, credential-bearing form, and the two combine.

Each workload can select up to 16 policies, each containing 1–64 destinations.
The compiler combines them with inferred integration access, removes equivalent
origins, and orders the result deterministically. The final Actor policy is
limited to 256 rules. References are additive; they cannot restrict inferred
model, MCP, artifact, or control-plane access. No references means no additional
access. A standalone sandbox with no references has no allowed egress.

## Call an API with a credential

Policies authenticate requests using a header, an optional prefix, and a Secret
in the same namespace. Authentication requires an exact HTTPS origin, and binds
the credential to that origin's hostname
(`ate-secret://k8s.io/default/<policy namespace>/<Secret>/<key>`):

```yaml
apiVersion: api.kagent.dev/v1alpha3
kind: EgressPolicy
metadata:
  name: ticket-api
  namespace: team-a
spec:
  destinations:
    - origin: https://tickets.example.com
      authentication:
        header: Authorization
        prefix: "Bearer "
        secretKeyRef:
          name: ticket-auth
          key: token
    - origin: https://search.example.com:8443
      authentication:
        header: X-API-Key
        secretKeyRef:
          name: search-auth
          key: api-key
```

The gateway sets the header value to `prefix + secretValue`. Prefixes are literal:
include the trailing space in `"Bearer "`. Omitting the prefix or setting it to
an empty string uses the Secret value directly, as with an API-key header.

Create these Secrets separately. The compiler validates that each Secret and key
exists, but persists only the reference. Substrate's credential provider fetches
the value and the gateway replaces the request header. Scripts and SDKs must
send that header, using an inert placeholder:

```sh
curl -H 'Authorization: Bearer kagent-credential-injected' \
  https://tickets.example.com/tickets
curl -H 'X-API-Key: kagent-credential-injected' \
  https://search.example.com:8443/search
```

The gateway replaces an existing header; it does not add a missing one. Policy
selection does not automatically configure arbitrary tools or environment
variables. Clients must honor the projected gateway trust bundle. See
[credential injection](credential-injection.md) for provider namespace grants,
CA setup, inferred SDK placeholders, and rotation behavior.

Bindings use the hostname and a case-insensitive header name, as every other
gateway credential does: a binding applies to each allowed port of its hostname.
Identical bindings merge; conflicting Secret references or prefixes for the same
hostname/header fail preparation. Explicit and inferred bindings follow the same
rule. Caller-token passthrough cannot share its hostname with static gateway
credentials. Routing, framing, hop-by-hop, proxy-authentication, and Cookie
headers cannot carry credentials. At most 16 credential headers are supported per
hostname. Use distinct DNS names for origins that need different credentials.

`RemoteMCPServer.spec.headersFrom` applies the same credential-header rules to
Secret-backed entries during Kubernetes admission. Literal and ConfigMap-backed
headers do not use gateway credential injection. A server may configure up to
64 headers, with names of at most 256 characters.

An exact credential-bearing rule wins over a matching wildcard rule. Credentials
are never attached to wildcard rules. HTTP, HTTPS and TLS passthrough cannot
share a hostname pattern and port, because Substrate cannot distinguish that
rule selection.

## Preparation and lifecycle

KRT watches selected policies and their Secrets. Missing policies, missing
Secret keys, invalid destinations, and conflicting bindings prevent preparation.
The controller publishes the error on the workload's status and re-evaluates it
when dependencies change.

Resolved destinations and credential references enter the immutable revision.
Changing a grant or Secret reference changes that revision. Secret values and
UIDs do not; the gateway handles value rotation independently. Policy metadata
changes do not affect the resolved grant. Reference ordering does not affect
revision identity.

New Sessions and sandboxes pin a prepared revision. Existing instances keep
their original grant through retries and resume; Session checkpoint forks also
retain the source revision. Updating or deleting a policy is not live revocation.
Failed preparation does not overwrite a previous successful revision, and the
existing readiness rules prevent selecting stale inputs for new instances.

The runtime workflow creates a suspended Actor, installs its compiled `default`
egress policy, and resumes it only after installation succeeds. A retry uses the
same pinned revision. Golden Actor preparation and artifact-startup egress are
unchanged; this API controls runtime Actor access.

## Authorization and scope

Policies are namespace-scoped configuration. Administrators can grant separate
RBAC permissions to author EgressPolicy objects; kagent's controller needs only
get/list/watch. Workload authors who can select a policy in that namespace can
use its grant. There is no separate per-policy `use` authorization check. Treat
the namespace as the sharing boundary for selectable policies and credentials.

The same trust-boundary consideration applies to existing ModelConfig and
RemoteMCPServer Secret references. This resource does not turn the namespace
into a per-workload Secret isolation boundary. Credential injection keeps values
out of compiled inputs; it does not prevent an allowed destination from echoing
credentials back to a client. Do not grant credentials to destinations that
should not receive or disclose them.

This API intentionally does not expose Substrate rule effects, raw TCP/SSH,
OAuth exchange, request signing, cross-namespace references,
or unrestricted public-internet access. Substrate remains the enforcement
backend; kagent exposes task-oriented reusable grants and static authentication.
