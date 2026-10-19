# Runtime credential injection

Kagent requires Substrate **v0.6.0-alpha3**. The runtime compiler turns
ModelConfig API keys and Secret-backed RemoteMCPServer headers into pinned
destination-scoped bindings. Independently, EgressPolicy admission supplies
additional live bindings. Substrate's gateway fetches the referenced Kubernetes Secret and
replaces the outgoing HTTP header when the request carries a placeholder. SDKs
receive an inert placeholder where they require an API key; real credentials never enter compiled environments,
runtime configuration, or revision provenance.

Inferred integration bindings are persisted with the prepared revision; their
Secret names, keys, destinations, and headers affect revision identity. Explicit
EgressPolicy bindings are resolved from current policies and update live actors
without changing that revision. Conflicts hold new grants and report the
independent [EgressPoliciesApplied condition](egress-policies.md#preparation-and-lifecycle);
they do not block runtime compilation. Both are installed before execution, including
creation retries, resume, and checkpoint forks. Secret values and Secret UIDs
do not affect revision identity. Rotation is handled by the gateway; its credential cache can
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

Provider endpoint overrides determine the allowed HTTP(S) origin. Egress rules
and credential bindings both match its scheme, DNS name, and port. Different
credentials for the same origin and header are rejected, including conflicts
between models, memory embeddings, MCP servers, and explicit policies. Distinct
ports on the same DNS name can use different credentials. IP-address destinations
are unsupported by Substrate egress policies.

The v1alpha3 revision format stores full origins. It assumes a clean installation;
previously persisted hostname-only credential bindings are rejected rather than
reinterpreted across every port on that host.

Harness and SandboxTemplate environment entries accept only literal `value`
strings, including empty strings. Configure Secret-backed credentials on
ModelConfig, RemoteMCPServer, or [EgressPolicy](egress-policies.md) for gateway injection.

AWS IAM signing keys, Google service-account keys, and OAuth client credentials
require mechanisms beyond static header injection and are rejected rather than
serialized into runtimes.

Caller-token passthrough is set with `tokenPassthrough` on ModelConfig or
RemoteMCPServer. It forwards the bearer token from the incoming A2A request
instead of a gateway credential. A passthrough model or MCP server cannot share
an origin with static gateway credentials, which would override the caller's
authentication. A passthrough RemoteMCPServer cannot set `Authorization` in
`headersFrom`.

RemoteMCPServer passthrough is implemented only by the Go kagent runtime.
Codex and Claude reject it at compilation, and the Python runtime fails at
startup. A request without a caller token fails instead of connecting
anonymously. The runtime discovers tools on first use, because startup has no
caller token. Controller discovery also has no caller token, so servers that
require one should set the `kagent.dev/discovery: disabled` label. The runtime
shares one MCP session per server. The standalone SSE stream and session
teardown therefore carry the token of the caller that opened the session.

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

`apiKeySecret` remains available for supported gateway credentials. Secret-backed
AWS IAM signing, Vertex service-account, and SAP OAuth credentials still fail
compilation; sharing this field with supported providers does not enable those
credential modes.
