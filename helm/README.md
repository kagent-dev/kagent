# Kagent Helm Chart

These Helm charts install kagent-crds and kagent. The kagent-crds chart must be installed first.

## Installation

### Using Helm

```bash
# First, install the required CRDs
helm install kagent-crds ./helm/kagent-crds/  --namespace kagent

# Then install kagent with default provider
# --set providers.default=openAI is enabled by default, but you need to provide your OpenAI API key
helm install kagent ./helm/kagent/ --namespace kagent --set providers.openAI.apiKey=your-openai-api-key

# Or with optional providers if you prefer local ollama provider or anthropic
helm install kagent ./helm/kagent/ --namespace kagent --set providers.default=ollama
helm install kagent ./helm/kagent/ --namespace kagent --set providers.default=openAI       --set providers.openAI.apiKey=your-openai-api-key
helm install kagent ./helm/kagent/ --namespace kagent --set providers.default=anthropic    --set providers.anthropic.apiKey=your-anthropic-api-key
helm install kagent ./helm/kagent/ --namespace kagent --set providers.default=azureOpenAI  --set providers.azureOpenAI.apiKey=your-openai-api-key
helm install kagent ./helm/kagent/ --namespace kagent --set providers.default=mistral      --set providers.mistral.apiKey=your-mistral-api-key
```

### PostgreSQL

The Helm chart does not deploy or initialize PostgreSQL. It expects a prepared
database and connection Secrets. `kagent install` creates a development
PostgreSQL instance, prepares the identities below, and then invokes Helm.
To use `kagent install` with an already prepared database, pass
`--skip-database-setup` and provide the Secret names through
`KAGENT_HELM_EXTRA_ARGS` `--set` overrides.

Kagent and Substrate use separate schemas and identities in one database:

| Product access | User | Group role |
| --- | --- | --- |
| Kagent | `kagent_user` | `kagent_owner` |
| Substrate owner | `substrate_owner_user` | `substrate_owner` |
| Substrate read/write | `substrate_readwrite_user` | `substrate_readwrite` |

Direct Helm installs must create those users, group roles, schemas, grants, and
three connection Secrets before installing the chart. Enable Substrate with:

```yaml
substrate:
  enabled: true
```

Configure the pre-created Secrets and roles with:

```yaml
database:
  postgres:
    secretRef:
      name: kagent-postgres
      key: connectionString
substrate:
  enabled: true
  postgres:
    readWriteConnectionStringSecretRef:
      name: substrate-postgres-readwrite
      key: readWriteConnectionString
    ownerConnectionStringSecretRef:
      name: substrate-postgres-owner
      key: ownerConnectionString
    readWriteRole: substrate_readwrite
    ownerRole: substrate_owner
```

When vectors are enabled, install pgvector before migrations and set
`database.postgres.vectorSchema` to its schema. Grant the Kagent role `USAGE`
on that schema.

#### Credential rotation

An outside process rotates credentials. First, create a new user and grant the applicable group role.

Next, update the connection Secret. Kagent and Substrate read the Secret before each new physical connection.

Set each pool lifetime to limit old connection use. Keep both users valid during Secret projection and connection replacement.

A host, port, fallback target, or database change requires a restart.

Kagent 1.x removes `database.postgres.url` and `database.postgres.urlFile`. Replace either value:

```yaml
database:
  postgres:
    url: postgresql://user:password@database.example/kagent
```

with:

```yaml
database:
  postgres:
    secretRef:
      name: postgres-connection
      key: connectionString
```

This supports externally rotated Secret values. Minting an RDS IAM token in
process on every connection is separate work and requires equivalent hooks in
both Kagent and Substrate.

#### OIDC authentication

Set `controller.auth.mode: trusted-proxy` together with
`oauth2-proxy.enabled: true`. Set `controller.auth.userIdClaim: email` to use
email identities, or leave it empty to use `sub`. The chart renders
`KAGENT_AUTH_MODE` and `KAGENT_AUTH_USER_ID_CLAIM`, which the shipped controller
consumes at startup. Unsupported modes fail startup; `insecure` remains the
default.

Follow the [OIDC deployment configuration](../docs/architecture/oidc-proxy-authentication.md#deployment-configuration)
for provider credentials, callback URL, proxy ingress, and required network
isolation. The trusted controller decodes claims without verifying signatures
or expiry, so all public API, A2A, and MCP traffic must pass through the validating
proxy and UI nginx.

#### Selecting a Substrate sandbox

The default sandbox is `gvisor`. With Substrate configured, use these values
to select `microvm`:

```yaml
controller:
  substrate:
    enabled: true
substrateWorkerPool:
  create: true
  sandboxClass: microvm
  workerImage: <matching-microvm-worker-image>
```

Substrate worker images are published to GHCR, for example
`ghcr.io/kagent-dev/substrate/ateom-microvm:latest`. For a pinned installation,
use a release tag matching your Substrate version.

Reference the pool through `spec.substrate.workerPoolRef` on a Harness in the same namespace.

**Note**: MicroVM requires a `microvm` SandboxConfig, runtime assets, and KVM-capable
workers. kagent does not install these prerequisites.

### Using Make

```bash
# export your openAI key
export OPENAI_API_KEY=your-openai-api-key
export ANTHROPIC_API_KEY=your-anthropic-api-key
export AZURE_OPENAI_API_KEY=your-azure-api-key

# install the kagent charts with openAI provider 
make KAGENT_DEFAULT_MODEL_PROVIDER=openAI helm-install

# install charts with anthropic provider
make KAGENT_DEFAULT_MODEL_PROVIDER=anthropic helm-install

# install charts with azureOpenAI provider
make KAGENT_DEFAULT_MODEL_PROVIDER=azureOpenAI helm-install

# install charts with ollama provider
make KAGENT_DEFAULT_MODEL_PROVIDER=ollama helm-install
```

The Make target regenerates protobuf bindings, rebuilds all local images, and
rolls the controller and UI before installing. Native gRPC, gRPC-Web, A2A, MCP,
and operational HTTP endpoints share controller port `8083`.

### Using kagent cli

```bash
## make sure have env variable with your API_KEY
export OPENAI_API_KEY=your-openai-api-key
export ANTHROPIC_API_KEY=your-anthropic-api-key
export AZURE_OPENAI_API_KEY=your-azure-api-key

#default provider is openAI but you can select from the list 
export KAGENT_DEFAULT_MODEL_PROVIDER=ollama
export KAGENT_DEFAULT_MODEL_PROVIDER=azureOpenAI
export KAGENT_DEFAULT_MODEL_PROVIDER=anthropic

# use local helm chart to install kagent with openAI provider
export KAGENT_DEFAULT_MODEL_PROVIDER=openAI
export KAGENT_HELM_REPO=./helm/
make kagent-cli-install

# use local helm chart to install kagent with ollama provider
export KAGENT_DEFAULT_MODEL_PROVIDER=ollama
export KAGENT_HELM_REPO=./helm/
make kagent-cli-install

```

## Upgrading

When upgrading, make sure to upgrade both charts:

```bash
# First, upgrade the CRDs
helm upgrade kagent-crds ./helm/kagent-crds/  --namespace kagent

# Then upgrade Kagent
helm upgrade kagent ./helm/kagent/ --namespace kagent
```

## Uninstallation

To properly uninstall Kagent:

```bash
# First, uninstall Kagent
helm uninstall kagent --namespace kagent

# To completely remove all resources including CRDs (optional):
helm uninstall kagent-crds --namespace kagent
```

**Note**: Uninstalling the CRDs chart will delete all custom resources of those types across all namespaces.

## Why Separate CRDs?

Helm has a limitation where CRDs are installed but not removed during uninstallation. 
By separating CRDs into their own chart, we can:

1. Allow proper version control of CRDs
2. Enable users to choose when to remove CRDs (which is destructive)
3. Follow Helm best practices
