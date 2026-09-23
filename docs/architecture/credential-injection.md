# Runtime credential injection

Kagent requires Substrate **v0.2.0-beta5**. The compiler turns ModelConfig API
keys and Secret-backed RemoteMCPServer headers into destination-scoped egress
bindings. Substrate's gateway fetches the referenced Kubernetes Secret and
overwrites the outgoing HTTP header. SDKs receive an inert placeholder where
they require an API key; real credentials never enter compiled environments,
runtime configuration, or revision provenance.

Bindings are persisted with the prepared revision and installed before an
instance becomes ready, including retries and checkpoint forks. Secret names,
keys, destinations, and headers affect revision identity. Secret values and
Secret UIDs do not. Rotation is handled by the gateway; its credential cache can
take up to five minutes to refresh, without recompiling or restarting an agent.

## Installation

The beta4 chart installs the credential provider and HTTPS interception gateway.
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
`ate-secret://kubernetes.io/kagent/model-auth/api-key`.

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
| Google service account key (Vertex AI, Claude Harness) | `authorization: Bearer <access token>`; Substrate mints the token from the key at fetch time |
| RemoteMCPServer Secret-backed header | Configured header; Secret contains its full value |

Provider endpoint overrides determine the injection destination. Substrate
matches exact DNS hostnames, without path, port, or scheme scoping. Different
credentials for the same hostname and header are rejected, including conflicts
between models, memory embeddings, and MCP servers. Use distinct DNS names for
origins requiring different credentials. IP-address destinations cannot carry
credential injection rules.

Google service account keys are the one credential the gateway transforms rather
than copies. The compiler binds an `AnthropicVertexAI` ModelConfig to
`ate-secret://google-access-token.kubernetes.io/<namespace>/<secret>/<key>` on the
Vertex AI hostname for its location, and Substrate's Kubernetes credential
provider signs the key's JWT assertion and exchanges it for a `cloud-platform`
access token when the gateway fetches the credential. The runtime never holds the
key: Claude Code runs with `CLAUDE_CODE_SKIP_VERTEX_AUTH=1` and sends the request
unauthenticated for the gateway to complete. Tokens live an hour and the provider
refreshes them ahead of the gateway cache, so rotating the key needs no
recompilation. This needs a Substrate release whose provider serves the
`google-access-token.kubernetes.io` name. The kagent runtimes still authenticate
to Vertex AI themselves, so a Vertex ModelConfig with a Secret remains rejected
for them.

AWS IAM signing keys, OAuth client credentials, and arbitrary Harness
`credentialRef` environment values require mechanisms beyond static header
injection and are rejected rather than serialized into runtimes. Caller-token
passthrough retains its existing behavior. A passthrough model cannot share a
hostname with static gateway credentials, which would override the caller's
authentication.
