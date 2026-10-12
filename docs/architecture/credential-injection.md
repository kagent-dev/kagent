# Runtime credential injection

Kagent requires Substrate **v0.4.0-alpha1**. The compiler turns ModelConfig API
keys and Secret-backed RemoteMCPServer headers into destination-scoped egress
bindings. Substrate's gateway fetches the referenced Kubernetes Secret and
replaces the outgoing HTTP header when the request carries a placeholder. SDKs
receive an inert placeholder where they require an API key; real credentials never enter compiled environments,
runtime configuration, or revision provenance.

Bindings are persisted with the prepared revision and installed before a
session becomes ready, including retries and checkpoint forks. Secret names,
keys, destinations, and headers affect revision identity. Secret values and
Secret UIDs do not. Rotation is handled by the gateway; its credential cache can
take up to five minutes to refresh, without recompiling or restarting an agent.

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

## Supported credentials

| Source | Header |
| --- | --- |
| OpenAI API key | `authorization: Bearer <key>` |
| Anthropic API key | `x-api-key: <key>` |
| Azure OpenAI and Foundry OpenAI API key | `api-key: <key>` |
| Foundry Anthropic API key | `x-api-key: <key>` |
| Gemini API key | `x-goog-api-key: <key>` |
| Bedrock bearer token | `authorization: Bearer <token>` |
| RemoteMCPServer Secret-backed header | Configured header; Secret contains its full value |
| Harness `env[].credentialRef` | Configured header and prefix; Secret key is the value |

Provider endpoint overrides determine the allowed HTTP(S) origin. Egress rules
match its scheme, DNS name, and port; credential bindings remain scoped to the
hostname and header. Different credentials for the same hostname and header
are rejected, including conflicts between models, memory embeddings, and MCP
servers. Use distinct DNS names for
origins requiring different credentials. IP-address destinations are unsupported
by Substrate egress policies.

SandboxTemplate environment entries accept only literal `value` strings,
including empty strings. Harness environment entries (including an Agent's
inline Harness) accept either a literal `value` or a `credentialRef`.

### Harness environment credentials

`credentialRef` gives a runtime an API key or bearer token for an HTTP API
that is neither a model nor an MCP server:

```yaml
spec:
  env:
    - name: TICKETS_API_TOKEN
      credentialRef:
        name: tickets-auth            # Secret in the Harness namespace
        key: token
        url: https://tickets.example.com/api
        header: Authorization
        prefix: "Bearer "
```

The compiler sets `TICKETS_API_TOKEN` to the inert placeholder, binds
`ate-secret://k8s.io/default/<namespace>/tickets-auth/token` to the URL's
hostname and header, and adds the URL's origin to the egress allow-list. The
runtime must send the header (any value, such as the variable itself); the
gateway replaces it with the prefix and the current Secret value. A missing
Secret or key reports `ResolvedRefs=False` on the Agent.

The value never enters the ActorTemplate, so it is never captured by the
golden snapshot that every session of the agent restores from, nor by later
pause or suspend snapshots. Rotating the Secret takes effect within the gateway
cache refresh, without a new revision or golden snapshot.

Limits:

- Only HTTP header injection is supported. Values the process must read
  itself (AWS web-identity token files, SigV4 or other signing keys, client
  certificates, non-HTTP protocols) cannot be provided: Substrate has no
  per-actor, restore-time secret projection yet, and any template-time value
  would be frozen into the shared golden snapshot.
- A binding covers every request to its hostname that carries the header,
  whatever the path, so an agent can use the credential anywhere on that host.
  The placeholder is not a secret and is not checked by the gateway.
- Each `credentialRef` adds exactly one origin to the egress allow-list; other
  destinations still require a model, MCP server, or skill source.

AWS IAM signing keys, Google service-account keys, and OAuth client credentials
require mechanisms beyond static header injection and are rejected rather than
serialized into runtimes.
Caller-token passthrough retains its existing behavior. A passthrough model
cannot share a hostname with static gateway credentials, which would override
the caller's authentication.

## Deferred API fields

The source retains commented declarations for ModelConfig
`openAI.tokenExchange` and TLS `caCertSecretRef`, `caCertSecretKey`, and
`disableSystemCAs`. These fields are absent from the served CRDs until their
runtime paths are implemented. The
RemoteMCPServer TLS rotation `status.secretHash` is deferred with custom CAs.

ModelConfig and RemoteMCPServer TLS settings currently expose only
`disableVerify`, supported by the kagent compiler. Codex and Claude reject model
TLS settings and warn when ignoring RemoteMCPServer TLS settings. Runtime trust
for gateway injection is configured by the platform.

`apiKeySecret` remains available for supported gateway credentials. Secret-backed
AWS IAM signing, Vertex service-account, and SAP OAuth credentials still fail
compilation; sharing this field with supported providers does not enable those
credential modes.
