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
| Google service account key (Vertex AI) | `authorization: Bearer <access token>`; Substrate mints the token from the key at fetch time |
| RemoteMCPServer Secret-backed header | Configured header; Secret contains its full value |

Provider endpoint overrides determine the allowed HTTP(S) origin. Egress rules
match its scheme, DNS name, and port; credential bindings remain scoped to the
hostname and header. Different credentials for the same hostname and header
are rejected, including conflicts between models, memory embeddings, and MCP
servers. Use distinct DNS names for
origins requiring different credentials. IP-address destinations are unsupported
by Substrate egress policies.

Harness and SandboxTemplate environment entries accept only literal `value`
strings, including empty strings. Configure Secret-backed credentials on
ModelConfig or RemoteMCPServer for gateway injection.

Google service account keys are the one credential the gateway transforms rather
than copies. The compiler binds a `GeminiVertexAI` or `AnthropicVertexAI`
ModelConfig to `ate-secret://google-access-token.k8s.io/default/<namespace>/<secret>/<key>`
on the Vertex AI hostname for its location, and Substrate's Kubernetes credential
provider signs the key's JWT assertion and exchanges it for a `cloud-platform`
access token when the gateway fetches the credential. The runtime never holds the
key: the Go ADK runs with `KAGENT_SKIP_VERTEX_AUTH=true` and Claude Code with
`CLAUDE_CODE_SKIP_VERTEX_AUTH=1`, and each sends the request for the gateway to
complete. Tokens live an hour and the provider refreshes them ahead of the
gateway cache, so rotating the key needs no recompilation. The compiler checks
the key when the agent is compiled (a service account key of the ModelConfig's
project, with Google's token endpoint), so a misconfigured Secret is reported
against the AgentTemplate rather than at fetch time. This needs a Substrate
release whose provider serves the `google-access-token.k8s.io` authority.

AWS IAM signing keys and OAuth client credentials require mechanisms beyond
static header injection and are rejected rather than serialized into runtimes.
Caller-token passthrough retains its existing behavior. A passthrough model
cannot share a hostname with static gateway credentials, which would override
the caller's authentication.

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
AWS IAM signing and SAP OAuth credentials still fail compilation; sharing this
field with supported providers does not enable those credential modes.
