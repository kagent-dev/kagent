# Runtime credential injection

ModelConfig, RemoteMCPServer, and EgressPolicy use the same `headers`
contract. Each header has exactly one source: a same-namespace Secret key or the
authenticated invocation's token. Integration endpoints determine destinations;
there is no need to repeat their hosts in an EgressPolicy.

Kagent requires Substrate **v0.7.0-alpha1**.

```yaml
headers:
  - name: Authorization
    prefix: "Bearer "
    valueFrom:
      tokenPassthrough: {}
```

Within `valueFrom`, replace `tokenPassthrough: {}` with
`secretKeyRef: {name: model-access, key: api-key}`. Header names and prefixes are
explicit; the compiler supplies inert SDK API keys and placeholders where a
provider requires them. For example, OpenAI commonly uses `Authorization` with
`"Bearer "`, Anthropic uses `x-api-key`, Azure uses `api-key`, and Gemini uses
`x-goog-api-key`. The former `apiKeySecret`, RemoteMCPServer `headersFrom`, and top-level
`tokenPassthrough` fields are replaced in this clean-install v1alpha3 API.
Each `valueFrom` requires exactly one of the two sources; literal `value` is not
supported. ModelConfig `defaultHeaders` remain separate and cannot override a
credential header. RemoteMCPServer has only the shared credential header API.

Bindings match the complete HTTP(S) origin (scheme, DNS name, and port) and a
case-insensitive header name. Identical bindings merge; conflicting sources or
prefixes are rejected. Routing, framing, proxy, and Cookie headers cannot carry
credentials. The contract does not grant path-level isolation within an origin.
IP-address destinations and wildcard credential bindings are unsupported.

Token passthrough from integrations and EgressPolicies shares one grant admission
model, including revoke-before-grant convergence and the
[EgressPoliciesApplied condition](egress-policies.md#preparation-and-lifecycle).
Successful compilation persists the applied integration source UIDs and bindings
independently of runtime readiness. Failed compilation preserves that applied
configuration. Live source deletion, replacement, or valid header removal revokes
access for subsequent invocations; malformed source configuration preserves its
last accepted bindings and reports an issue. EgressPolicy grants resolve
independently. Integration Secret bindings and required network destinations
remain revision inputs.

At dispatch, the control plane snapshots the effective header bindings for the
Session's prepared revision into private A2A transport metadata. Go and Python
ADKs apply that snapshot for the invocation, including policy-only bindings.
Their compiled configuration contains endpoints, ordinary settings, and inert SDK
keys where required, but no duplicate credential-header instructions. Native
harnesses retain gateway injection placeholders in their own configuration.

## Secret injection

Kagent requires Substrate **v0.6.0**. Substrate's gateway retrieves the Secret
and replaces an existing outgoing header. Secret values never enter compiled
environments, runtime configuration, or revision provenance. Name, key, origin,
header, and prefix affect revision identity; the Secret's value and UID do not.
Rotation can take up to five minutes to reach the gateway cache.

Kagent clients supply placeholders automatically. Arbitrary scripts must send the
configured header, for example `Authorization: Bearer kagent-credential-injected`.
The gateway does not add a header that is absent. Harness and SandboxTemplate
environment entries accept literal values only. AWS signing keys, Google service
account keys, and OAuth client credentials require other mechanisms and are
rejected by the credential header compiler.

## Installation

The Substrate chart installs the credential provider and HTTPS interception gateway.
Grant each agent atespace access to its credential namespace in the Substrate
release values:

```yaml
credentialProvider:
  namespacePolicies:
    - atespace: kagent
      allowedNamespaces: [kagent]
```

For an embedded Substrate chart, put these values under `substrate:` in the
kagent chart. Empty grants deny all credential fetches. Kagent compiles
same-namespace references such as
`ate-secret://k8s.io/default/kagent/model-auth/api-key`.

Create the gateway CA in the Substrate release namespace before waiting for
the rollout (alongside the other Substrate CA pools):

```sh
kubectl-ate admin make-ca-pool --ca-id=1 --name=egress-mitm-ca-pool \
  --secret-namespace=ate-system --key-type=ECDSAP256
```

The local setup script and CI install both the grant and CA. ActorTemplates
project `egress-mitm.ate.dev` into `/run/kagent/egress/trust-bundle.pem` and set
the Go/OpenSSL, Node, Python requests, AWS, curl, and git CA environment variables.
Custom BYO clients must honor the configured trust bundle. Harness environment
overrides of these variables are rejected.

## Invocation token passthrough

Passthrough uses the authenticated caller of this invocation, whether human or
machine. It never looks up a token from the Session creator. The scheduler uses
a verified kagent service-account credential with a separate downstream audience.
A later human continuation
uses that human's credential while retaining the conversation and creator.
See [runtime authority](a2a-gateway.md#runtime-authority).

The control plane forwards the authenticated invocation's caller credential in
private A2A metadata. Go and Python retain it only in invocation-local context.
MCP connections capture the caller that opened them, including background streams
and teardown, and close at invocation completion. Models use the current
invocation. Credentials never enter ADK Session state, runtime configuration,
environment variables, Kubernetes objects, or database records.

Projected SA credentials are read and verified when forwarding each new
invocation, including audience and SA UID checks. Kubelet rotation supplies later
invocations; an existing invocation keeps its original token. Token passthrough
does not renew or exchange tokens. Calls relying on an expired token fail at the
destination, and a new authenticated invocation is needed to supply a new token.

The snapshot uses `x-kagent-invocation-headers-bin`, a binary gRPC metadata entry
containing JSON origins, header names, prefixes, and injection placeholders. It
contains no token bytes. The caller credential travels separately in private
transport metadata. The control plane replaces caller-supplied binding metadata;
ADKs keep the decoded bindings in invocation-local context. Neither bindings nor
credentials become public A2A message metadata or durable Session state. There is
no runtime policy fetch, token registry, credential reference, or controller
replica affinity.

Both integration and EgressPolicy passthrough changes take effect on the next
invocation. An already-running invocation retains its snapshot and token until
completion or expiry. The egress gateway continues enforcing current network and
injected-credential rules, but it does not independently revoke a plain forwarded
caller header while its destination remains allowed.

Failure to resolve policy prevents dispatch. An active passthrough binding without
a caller credential fails before sending to its destination; an absent binding
forwards no caller token. Managed SDK clients do not follow credential-bearing
redirects. Downstream services must trust the caller's issuer and accept the
token's audience and permissions.

The Claude and Codex harnesses do not support caller-token passthrough. Their
compilers reject ModelConfig and RemoteMCPServer passthrough bindings. Secret headers continue to use gateway injection. There is no
invocation HTTP proxy or caller token written to native process configuration.

Tool discovery requiring passthrough needs an authenticated invocation. The
controller does not substitute its own credential during global discovery;
set `kagent.dev/discovery: disabled` for such MCP servers. Runtime discovery
occurs in the caller's invocation context.

## Deferred API fields

The source retains commented declarations for Harness and SandboxTemplate
`env[].credentialRef`, ModelConfig `openAI.tokenExchange`, and TLS
`caCertSecretRef`, `caCertSecretKey`, and `disableSystemCAs`. These fields are
absent from the served CRDs until their runtime paths are implemented. The
RemoteMCPServer TLS rotation `status.secretHash` is deferred with custom CAs.

ModelConfig and RemoteMCPServer TLS settings currently expose only
`disableVerify`, supported by the kagent compiler. Codex and Claude reject model
TLS settings and warn when ignoring RemoteMCPServer TLS settings. Runtime trust
for gateway injection is configured by the platform.
