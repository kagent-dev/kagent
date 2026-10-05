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

The Kagent chart does not deploy or initialize PostgreSQL, and it does not install Substrate. A direct Helm install needs a prepared database, a connection Secret in the Kagent namespace, and Substrate installed as its own release.

`kagent install` does all of this for development: it installs Substrate in `ate-system`, deploys a PostgreSQL instance in the Kagent namespace, creates the identities and schemas below, writes the connection Secrets, and then installs Kagent. Kagent, Substrate, and PostgreSQL authenticate with Pod Certificates rather than passwords. The development database has no pgvector extension and is not intended for production. It pulls `postgres:18-alpine` from Docker Hub; set `KAGENT_BUNDLED_POSTGRES_IMAGE` to use a mirror.

To run `kagent install` against a database you prepared yourself, pass `--skip-database-setup` and point each chart at your Secrets through `KAGENT_HELM_EXTRA_ARGS` and `KAGENT_SUBSTRATE_HELM_EXTRA_ARGS`.

`kagent install` creates Substrate's certificate authorities once, and they expire one year later. Nothing renews them, and re-running `kagent install` keeps the existing ones. To replace them, delete the CA Secrets, re-run `kagent install`, and restart the Substrate and Kagent workloads:

```bash
kubectl delete secret -n ate-system actor-id-ca-pool actor-id-ca-certs egress-mitm-ca-pool
kubectl delete secret -n podcertificate-controller-system service-dns-ca-pool pod-identity-ca-pool postgres-ca-pool
```

Kagent and Substrate share one database with separate identities and schemas:

| Access | Login user | Assumed role | Schema |
| --- | --- | --- | --- |
| Kagent | `kagent_user` | `kagent_owner` | `kagent` |
| Substrate migrations | `substrate_owner_user` | `substrate_owner` | `substrate` |
| Substrate runtime | `substrate_readwrite_user` | `substrate_readwrite` | `substrate` |

#### Kagent values

```yaml
database:
  postgres:
    # Secret holding the connection string. Required.
    connectionStringSecretRef:
      name: kagent-postgres
      key: connectionString
    # Role assumed on every connection. Set to "" to use the login user directly.
    role: kagent_owner
    # Schema for Kagent tables. It must exist, or the role must be able to create it.
    schema: kagent
    # Schema where pgvector is installed. Used only when vectorEnabled is true.
    vectorSchema: extensions
    vectorEnabled: false
    # Project the kagent_user Pod Certificate at /run/postgres.podcert.ate.dev.
    # Only for databases that trust the Substrate postgres CA.
    clientCertificate:
      enabled: false
```

With an external database, either create the `kagent_owner` role and grant it to the login user, or set `role: ""`.

When vectors are enabled, install pgvector into `vectorSchema` before Kagent starts, and grant the Kagent role `USAGE` on that schema. Migrations do not create the extension, and startup fails if it is installed in a different schema.

The Substrate chart reads its own connection Secrets. See that chart's `postgres.ownerConnectionStringSecretRef` and `postgres.readWriteConnectionStringSecretRef` values.

#### Credential rotation

Kagent reads the connection string from the Secret once, at startup. After the Secret changes, restart the controller.

When the connection string names TLS certificate files, Kagent rereads them before each new physical connection. Rotated client certificates, such as Pod Certificates, take effect without a restart. `database.postgres.pool.maxConnLifetime` limits how long a connection opened with an older certificate stays in use.

#### Migrating from `url` and `urlFile`

Kagent 1.x removes `database.postgres.url` and `database.postgres.urlFile`. Move the connection string into a Secret and reference it:

```yaml
database:
  postgres:
    connectionStringSecretRef:
      name: postgres-connection
      key: connectionString
```

Kagent 1.x also stores its tables in the `kagent` schema instead of `public`, so an upgraded installation starts with empty tables. Only clean installs are supported.

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

The worker pods run as the `<substrateWorkerPool.name>-worker` ServiceAccount, which the
chart creates. To use your own ServiceAccount, set
`substrateWorkerPool.template.serviceAccountName`. It must exist in the release namespace.

**Note**: MicroVM requires a `microvm` SandboxConfig, runtime assets, and KVM-capable
workers. kagent does not install these prerequisites.

### Using Make

```bash
export OPENAI_API_KEY=your-openai-api-key

# Build the local images, then install them with Substrate and the development database
make build
make KAGENT_DEFAULT_MODEL_PROVIDER=openAI kagent-cli-install
```

`make kagent-cli-install` builds the local CLI and runs `kagent install` against the local charts. `make helm-install` installs the charts directly with Helm. That needs a prepared database and `kagent-postgres` Secret (see [PostgreSQL](#postgresql)) and a separately installed Substrate. Native gRPC, gRPC-Web, A2A, MCP, and operational HTTP endpoints share controller port `8083`.

### Using kagent cli

```bash
export OPENAI_API_KEY=your-openai-api-key
# openAI is the default; other choices include anthropic, azureOpenAI, gemini, and ollama
export KAGENT_DEFAULT_MODEL_PROVIDER=openAI
kagent install
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
