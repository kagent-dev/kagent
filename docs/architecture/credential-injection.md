# Runtime credential injection

Kagent requires Substrate **v0.5.0-alpha2**. The runtime compiler turns
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
`ModelConfig.spec.tokenPassthrough: true` enables caller-token passthrough
instead of a static model API key. For example:

```yaml
spec:
  provider: OpenAI
  model: gpt-4.1
  tokenPassthrough: true
```

A passthrough model
cannot share an origin with static gateway credentials, which would override
the caller's authentication.

## Remote MCP caller bearer tokens

`RemoteMCPServer.spec.tokenPassthrough` is optional and defaults to
`false`. When enabled, the server receives `Authorization: Bearer <token>`
from the **current authenticated A2A invocation**. Go ADK reads only
`models.PassthroughToken` (the restricted `BearerTokenKey`); Python reads the
dedicated `bearer_token` ContextVar. Neither path falls back to general request
metadata, an STS token, a runtime service credential, or a previous invocation.
A missing token fails before an MCP connection starts.
A resumed/approved call requires the new invocation's token.

```yaml
apiVersion: api.kagent.dev/v1alpha3
kind: RemoteMCPServer
metadata:
  name: caller-tools
  namespace: kagent
  labels:
    kagent.dev/discovery: disabled
spec:
  description: Tools authorized by the invoking caller
  url: https://tools.example.com/mcp
  tokenPassthrough: true
```

Controller discovery has no caller identity:

- The existing `kagent.dev/discovery: disabled` label takes precedence. It
  retains `Accepted=True`, with no discovered tools or catalog connectivity.
- Without that label, passthrough requires an `Authorization` entry in
  `headersFrom` (case-insensitive) for discovery. Without one, the controller
  does not dial, clears stale discovered tools and catalog connectivity, and
  reports `Accepted=False`, reason `DiscoveryCredentialRequired`. It uses the
  ordinary discovery refresh interval.
- When supplied, that Authorization is **discovery-only**. All case variants
  are removed before runtime header resolution, including ConfigMap lookup,
  Secret environment placeholders, and gateway credential compilation. Other
  configured headers remain unchanged. Discovery failures do not publish
  upstream errors that could echo the credential.

Model and MCP passthrough destinations cannot share an origin with **any**
static gateway credential, including an MCP server's own Secret-backed
non-Authorization header. Matching lowercases DNS names and removes a trailing
dot and normalizes default ports, just as egress credential canonicalization
does. Different schemes or ports are separate origins; paths are not.
Multiple passthrough users of one origin are
allowed. Discovery-only Authorization creates no gateway binding.

The ADK protobuf carries only the flag, not a token. Go ADK uses a fresh MCP
session for each listing and tool call, closing it at the operation boundary:
the ordinary ADK toolset retains its first caller's session context and is not
safe for this mode. Python creates agents per request and pools MCP sessions
by effective headers within that lifetime. Request headers use ADK's
`temp:headers` state, so credentials are not written to its native SQLite
conversation or event history. Go ADK and Python passthrough HTTP clients reject
redirects and foreign SSE endpoint destinations.

This setting is supported only by Go and Python ADK. Claude and Codex
compilers reject it; native runtime support is not implemented.

This per-server setting does not enable the older global ADK token-propagation
switch. That independently configured behavior remains unchanged for servers
without passthrough; it cannot override a passthrough server's credential.

MCP Apps `CallTool` and `ReadResource` control-plane calls do not carry the
restricted A2A invocation context. They are unsupported for passthrough
servers and fail before resolving discovery credentials or opening a
connection. The controller must never use discovery Authorization to perform
an app's tool call.

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
