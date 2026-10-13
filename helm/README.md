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

`kagent install` does all of this for development: it installs Substrate in `ate-system`, deploys a PostgreSQL instance in the Kagent namespace, creates the identities and schemas below, writes the connection Secrets, and then installs Kagent. Kagent, Substrate, and PostgreSQL authenticate with Pod Certificates rather than passwords. The development database has no pgvector extension and is not intended for production. It pulls `postgres:18-alpine` from Docker Hub; set `install.bundledPostgresImage` in the CLI config to use a mirror.

To run `kagent install` against a database you prepared yourself, pass `--skip-database-setup` and point each chart at your Secrets through `install.kagent.valuesFiles` and `install.substrate.valuesFiles` in the CLI config.

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
    # Project the kagent_user Pod Certificate at /run/postgres.podcert.ate.dev.
    # Only for databases that trust the Substrate postgres CA.
    clientCertificate:
      enabled: false
config:
  database:
    role: kagent_owner
    schema: kagent
    vectorSchema: extensions
    vectorEnabled: false
```

With an external database, either create the `kagent_owner` role and grant it to the login user, or set `config.database.role: ""`. The role needs access to `config.database.schema`, which must exist or be creatable by that role.

When vectors are enabled, install pgvector into `config.database.vectorSchema` before Kagent starts, and grant the Kagent role `USAGE` on that schema. Migrations do not create the extension, and startup fails if it is installed in a different schema.

The Substrate chart reads its own connection Secrets. See that chart's `postgres.ownerConnectionStringSecretRef` and `postgres.readWriteConnectionStringSecretRef` values.

#### Credential rotation

Kagent reads the connection string from the Secret once, at startup. After the Secret changes, restart the controller.

When the connection string names TLS certificate files, Kagent rereads them before each new physical connection. Rotated client certificates, such as Pod Certificates, take effect without a restart. `config.database.pool.maxConnLifetime` limits how long a connection opened with an older certificate stays in use.

#### OIDC authentication

Set `config.server.auth.mode: trusted-proxy` together with
`oauth2-proxy.enabled: true`. Set `config.server.auth.userIdClaim: email` to use
email identities, or leave it empty to use `sub`. The chart writes these settings into `controller.yaml`, which the controller
validates at startup. Unsupported modes fail startup; `insecure` remains the
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
substrateWorkerPool:
  create: true
  sandboxClass: microvm
  workerImage: <matching-microvm-worker-image>
```

Substrate worker images are published to GHCR, for example
`ghcr.io/kagent-dev/substrate/ateom-microvm:latest`. For a pinned installation,
use a release tag matching your Substrate version.

Reference the pool through `spec.substrate.workerPoolRef` on a Harness in the same namespace.

The worker pods run as a ServiceAccount with the name `substrateWorkerPool.name`, which the
chart creates. To use your own ServiceAccount, set `substrateWorkerPool.serviceAccount.create`
to `false` and `substrateWorkerPool.serviceAccount.name` to its name. It must exist in the
release namespace.

**Note**: MicroVM requires a `microvm` SandboxConfig, runtime assets, and KVM-capable
workers. kagent does not install these prerequisites.

### Using Make

```bash
export OPENAI_API_KEY=your-openai-api-key

# Build the local images, then install them with Substrate and the development database
make build
make MODEL_PROVIDER=openAI kagent-cli-install
```

`make kagent-cli-install` builds the local CLI and runs `kagent install` against the local charts. `make helm-install` installs the charts directly with Helm. That needs a prepared database and `kagent-postgres` Secret (see [PostgreSQL](#postgresql)) and a separately installed Substrate. Native gRPC, gRPC-Web, A2A, MCP, and operational HTTP endpoints share controller port `8083`.

### Using kagent cli

```bash
export OPENAI_API_KEY=your-openai-api-key
# openAI is the default; other choices include anthropic, azureOpenAI, gemini, and ollama
# config.yaml: install: {defaultModelProvider: OpenAI}
kagent --config config.yaml install
```

## Application configuration

`config` follows the [ControllerConfig schema](../proto/kagent/api/v1alpha1/config.proto).
It is rendered as `/etc/kagent/controller.yaml`; secret files are projected separately.
Configure controller and agent telemetry independently using `config.telemetry`
and `config.runtimeDefaults.telemetry`. Standard OTEL overrides can be supplied
through `controller.env`/`envFrom` for this controller process only.

See [process configuration](../docs/architecture/process-configuration.md) for examples,
validation commands, defaults, and runtime configuration.

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
