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
| AgentTemplate `skills[].source.git.authorizationFrom` | `authorization`; Secret contains its full value |

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

Compilation records each passthrough client as a target {origin, header,
prefix} in the revision. It rejects a client with no credential target (SAP AI
Core), a client that shares its origin with an MCP server or model that did not
opt in, and an explicit header that matches a target header.

RemoteMCPServer passthrough is implemented only by the Go kagent runtime.
Codex and Claude reject it at compilation, and the Python runtime fails at
startup. A request without a caller token fails instead of connecting
anonymously. The runtime discovers tools on first use, because startup has no
caller token. Controller discovery also has no caller token, so servers that
require one should set the `kagent.dev/discovery: disabled` label. The runtime
shares one MCP session per server. The standalone SSE stream and session
teardown therefore carry the token of the caller that opened the session.

### Gateway token passthrough

Without `app.Options.EgressPolicyCompilers`, runtimes forward the caller token
as described above. To keep it out of runtimes, an embedding program registers
compilers and its `Authenticator.UpstreamAuth` must replace the caller's
`Authorization` with `Bearer kagent-credential-injected`; registering compilers
alone changes nothing, and a forwarding authenticator still exposes the token.
The runtime forwards the placeholder as usual. Compilers run in order after the
built-in EgressPolicy compiler and may change anything it produced. Kagent then
validates the result, and every target must have a header. New create and resume
operations compile the policy before they are issued, so a failure stops them
before any lifecycle mutation. If a later compile or this validation fails, the
actor gets no egress until a retry succeeds. Kagent never names a credential provider.

This covers Go ADK MCP servers and models, OpenAI, Azure and Foundry memory
embeddings, and Python OpenAI and Anthropic models. Python MCP, Claude and
Codex still do not support passthrough.

## Private Git skill sources

A standalone skill can use a private HTTPS Git repository. Its Secret must be
in the AgentTemplate's namespace. The Secret owner grants access to one exact
HTTPS origin with the `kagent.dev/git-credential-origin` annotation.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: skills-auth
  annotations:
    kagent.dev/git-credential-origin: https://github.com
stringData:
  # GitHub: base64 of "x-access-token:<token>". Other forges may accept "Bearer <token>".
  authorization: Basic eC1hY2Nlc3MtdG9rZW46PHRva2VuPg==
---
apiVersion: api.kagent.dev/v1alpha3
kind: AgentTemplate
metadata:
  name: reviewer
spec:
  skills:
    - name: review
      source:
        git:
          url: https://github.com/acme/private-skills
          commit: 0123456789abcdef0123456789abcdef01234567
          authorizationFrom:
            name: skills-auth
            key: authorization
```

The compiler checks the Secret, its non-empty key, and its authorized origin.
Missing references or an incorrect grant set `ResolvedRefs=False`.
Changes to the Secret trigger recompilation, including references from shared
child templates. HTTPS, DNS name, and port must match the grant.
Wildcards, IP addresses, URL credentials, and path-scoped grants are unsupported.

The gateway replaces `Authorization` only when the request includes that header.
The runtime sends a placeholder scoped to the repository URL through Git's
`http.<url>.extraHeader` setting. Authenticated fetches reject redirects.
Neither the Git configuration nor the runtime receives the Secret value.

The gateway binding covers the whole origin, not just the repository path.
Actor tools can also send an authorization header to that origin.
Use a read-only token restricted to the required repositories.
Separate permission to write Secrets from permission to write AgentTemplates.
Template writers cannot grant a Secret to another origin through a template.

The annotation is a compilation-time grant. Removing it prevents new successful
compilations; it does not revoke credentials from existing pinned Actors.
Use the platform's credential and egress controls for immediate revocation.
Do not give the shared golden Actor unrestricted access to namespace Secrets.

`authorizationFrom` is unsupported on `plugins[]`. SSH authentication and
token exchange are also unsupported.

### When skills are fetched

Go ADK, Claude, and Codex attempt standalone Git and S3 fetches at startup.
The startup attempt has a 30-second limit. A failed attempt logs a warning
and permits startup to continue.

Before each task, the runtime retries skills that are not installed.
A failed fetch fails the task with `skills_unavailable: …`.
A later task retries the missing skills. Installed skills need no further
network access while their files remain available.

OCI sources use worker-mounted image files. The runtime installs these skills
during preparation and fails preparation if a mounted source is invalid.
Plugin packages also remain eager.

Golden preparation can lack network access or credential bindings.
The startup fallback permits preparation without fetching remote skills.
If startup fetching succeeds, snapshots can contain private skill files.
Snapshots do not contain the injected credential. Apply snapshot access controls
that protect the private repository content.

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
