# Runtime credential injection

Kagent requires Substrate **v0.4.0-alpha1**. The compiler turns ModelConfig API
keys, Secret-backed RemoteMCPServer headers, and explicit EgressPolicy
authentication into destination-scoped egress bindings. Substrate's gateway fetches the referenced Kubernetes Secret and
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
| ChatGPT login (`openAI.credentialType: chatgptOAuth`, Codex harness only) | `authorization: Bearer <access token>` and `chatgpt-account-id: <account id>` under the ChatGPT Codex backend's path |
| Anthropic API key | `x-api-key: <key>` |
| Azure OpenAI and Foundry OpenAI API key | `api-key: <key>` |
| Foundry Anthropic API key | `x-api-key: <key>` |
| Gemini API key | `x-goog-api-key: <key>` |
| Bedrock bearer token | `authorization: Bearer <token>` |
| Google service account key (Vertex AI) | `authorization: Bearer <access token>`; Substrate mints the token from the key at fetch time |
| RemoteMCPServer Secret-backed header | Configured header; Secret contains its full value |
| Git skill or plugin source `credentialRef` | `authorization: Basic <key>`; Secret contains base64 `<username>:<token>` |

A binding names the actors it is for. Model and MCP bindings have no scope and
serve every actor. A git source binding has scope `golden`: the golden boot
fetches skills and plugins, so the ActorTemplate's default policy carries the
binding, and a session's Actor policy (on create, on repoint to a newer
revision, and on its rollback) is compiled without it. The source's host stays
allowed from the sandbox, unauthenticated, so a shell tool or other client in
the session cannot spend the source's token. Substrate gives a new Actor its
template's default policy; session creation replaces it with the session's
before the Actor first runs, and refuses any other existing allowlist. The
scope is part of revision identity.

Provider endpoint overrides determine the allowed HTTP(S) origin. Egress rules
match its scheme, DNS name, and port; credential bindings remain scoped to the
hostname and header. Different credentials for the same hostname and header
are rejected, including conflicts between models, memory embeddings, MCP
servers, and explicit policies. Use distinct DNS names for
origins requiring different credentials. IP-address destinations are unsupported
by Substrate egress policies.

Harness and SandboxTemplate environment entries accept only literal `value`
strings, including empty strings. Configure Secret-backed credentials on
ModelConfig, RemoteMCPServer, or [EgressPolicy](egress-policies.md) for gateway injection.

Google service account keys are the one credential the gateway transforms rather
than copies. The compiler binds a `GeminiVertexAI` or `AnthropicVertexAI`
ModelConfig to `ate-secret://google-access-token.k8s.io/default/<namespace>/<secret>/<key>`
on the Vertex AI hostname for its location, and the Google access-token
credential provider, a Substrate credential provider plugin
([giantswarm/substrate-credential-providers](https://github.com/giantswarm/substrate-credential-providers))
deployed beside Substrate, signs the key's JWT assertion and exchanges it for a
`cloud-platform` access token when the gateway fetches the credential. The runtime never holds the
key: the Go ADK runs with `KAGENT_SKIP_VERTEX_AUTH=true` and Claude Code with
`CLAUDE_CODE_SKIP_VERTEX_AUTH=1`, and each sends the request with the placeholder
`Authorization: Bearer kagent-credential-injected` for the gateway to complete:
the gateway replaces a header the request carries and adds none. Tokens live an hour and the provider refreshes them ahead of the
gateway cache, so rotating the key needs no recompilation. The compiler checks
the key when the agent is compiled (a service account key of the ModelConfig's
project, with Google's token endpoint), so a misconfigured Secret is reported
against the AgentTemplate rather than at fetch time. This needs the plugin
deployed and the Substrate chart routing the `google-access-token.k8s.io`
authority to it (`credentialProvider.additionalProviders`); the Substrate line
served the authority from its bundled provider until 1.12.

AWS IAM signing keys and OAuth client credentials require mechanisms beyond
static header injection and are rejected rather than serialized into runtimes.

A ChatGPT login is the subscription sign-in of the Codex CLI: a short-lived
access token, a refresh token that rotates on every use, and the ChatGPT
account id, used against the ChatGPT Codex backend
(`https://chatgpt.com/backend-api/codex`, or `openAI.baseUrl`) rather than the
OpenAI API. A ModelConfig with `openAI.credentialType: chatgptOAuth` names the
Secret in `apiKeySecret` and leaves `apiKeySecretKey` unset; the Secret holds
the keys `access_token`, `refresh_token` and `account_id` and carries the label
`kagent.dev/chatgpt-login: "true"`. The Codex compiler
points a custom Codex provider at the backend instead of Codex's own ChatGPT
sign-in, sets no `OPENAI_API_KEY`, and has Codex send
`KAGENT_CHATGPT_ACCESS_TOKEN` as its bearer token and
`KAGENT_CHATGPT_ACCOUNT_ID` as `chatgpt-account-id`, both placeholders the
gateway replaces from the Secret; the backend's host is a compiled egress
destination. A login opens the whole ChatGPT account, so both bindings carry
the backend's path as their path prefix (`/backend-api/codex/`): a request
the sandbox sends elsewhere on the host carries no credential. The refresh
token is bound nowhere and reaches no actor.

Each refresh spends the refresh token and returns a new one, so two
refreshers on one login invalidate each other. The controller is the login's
one refresher: on the elected leader, keyed by the Secret (two ModelConfigs on
one Secret share it), it refreshes the access token at the OpenAI token
endpoint 30 minutes before the token's expiry (a quarter of its lifetime
before, for a shorter-lived token), ahead of the gateway's cache, and writes
the new access token and the rotated refresh token back into the same Secret
with an optimistic update, so a login written in the meantime wins. A rotation
changes no revision and restarts no agent.

The ModelConfig author names the Secret, and the refresher rewrites it, so a
login's Secret opts in: a Secret without the label `kagent.dev/chatgpt-login:
"true"` is never written, and its ModelConfig reports `ResolvedRefs` false with
the reason `ChatGPTLoginNotOptedIn`. The refresher writes as a ServiceAccount
of its own, `<release>-chatgpt-login-refresher`, which the controller
impersonates (`KAGENT_CHATGPT_LOGIN_REFRESHER_SERVICE_ACCOUNT`): the chart lets
the controller impersonate that account only, grants the account `update` on
Secrets and nothing else, and a ValidatingAdmissionPolicy admits its update
only to a Secret that carries the label before and after the write, so the
account can neither add nor remove it. On an API server without
ValidatingAdmissionPolicy the chart renders none, and the refresher's own check
is the gate. A login must not be shared with any
other client, a Codex CLI included: sign in once with
`codex login --device-auth` into a `CODEX_HOME` of its own and copy the
tokens from its `auth.json` into the Secret. The ADK runtimes reject the mode.
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
