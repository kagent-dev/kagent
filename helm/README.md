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

Substrate v0.6.0-alpha3 restricts API ingress to namespaces labeled
`ate.dev/api-access=true`. The CLI installer applies this label to the Kagent
namespace. For direct Helm installations, label the controller namespace before
starting Kagent:

```sh
kubectl label namespace kagent ate.dev/api-access=true --overwrite
```

Upgrade the Substrate CRDs, control plane, and workers together. This upgrade
requires a fresh development database and recreated Actors, Tags, and WorkerPools;
Substrate does not migrate the previous snapshot metadata. WorkerPools now use
`spec.sandboxClasses[]`, with an existing `configRef` of the same runtime class.

Runtime-facing URLs must use full DNS names because Substrate only permits DNS
queries matching the egress policy. The chart generates controller and API tool
URLs using `global.clusterDomain` (default `cluster.local`). Set this to your
cluster's DNS domain and use full names in custom model, MCP, and telemetry URLs.

Substrate alpha3 returns `NXDOMAIN` for denied DNS lookups, allowing musl-based
Codex and Claude clients to continue past Kubernetes search-domain expansions.
Service URLs no longer need the trailing-dot workaround. Continue using full
service names; short-name aliases are not inferred from egress policies.
Native retry caps remain temporary: even with the DNS fix, denied connections
can trigger minutes of native retries.

Alpha3 enables API authorization by default. `kagent install` grants its default
controller pod-certificate identity (`spiffe://cluster.local/ns/<namespace>/sa/kagent-controller`)
global owner access, which is required to create atespaces. Direct Helm installs
must grant their controller access through Substrate's access policy API or
`ateApi.authorization.bootstrapOwners`. If you customize the controller's
ServiceAccount, chart fullname, or certificate trust domain, set the matching
principal in `install.substrate.valuesFiles`; these files override the CLI's
bootstrap defaults. Preserve any operator principals you still need in that list.

Every agent runtime now requires CPU and memory limits. Defaults are 1 CPU and
1 GiB; configure `config.runtimeDefaults.cpu` and `config.runtimeDefaults.memory`
using Kubernetes quantities. Standalone sandboxes use `config.sandboxes.cpu` and
`config.sandboxes.memory`. Changing limits creates a new immutable revision.

To run `kagent install` against a database you prepared yourself (see [Preparing your own database](#preparing-your-own-database)), pass `--skip-database-setup` and point each chart at your Secrets through `install.kagent.valuesFiles` and `install.substrate.valuesFiles` in the CLI config.

`kagent install` creates Substrate's certificate authorities once, and they expire one year later. Nothing renews them, and re-running `kagent install` keeps the existing ones. To replace them, delete the CA Secrets, re-run `kagent install`, and restart the Substrate and Kagent workloads:

```bash
kubectl delete secret -n ate-system actor-id-ca-pool actor-id-ca-certs egress-mitm-ca-pool
kubectl delete secret -n podcertificate-controller-system service-dns-ca-pool pod-identity-ca-pool postgres-ca-pool
```

Kagent and Substrate share one database with separate identities and schemas:

| Access | Login user | Assumed role | Schema |
| --- | --- | --- | --- |
| Kagent migrations | `kagent_owner_user` | `kagent_owner` | `kagent` |
| Kagent runtime | `kagent_readwrite_user` | `kagent_readwrite` | `kagent` |
| Substrate migrations | `substrate_owner_user` | `substrate_owner` | `substrate` |
| Substrate runtime | `substrate_readwrite_user` | `substrate_readwrite` | `substrate` |

#### Kagent values

```yaml
database:
  postgres:
    # Owner login: creates the schema and runs migrations. Not used when config.database.skipMigrations is true.
    ownerConnectionStringSecretRef:
      name: kagent-postgres-owner
      key: ownerConnectionString
    # Read/write login: serves every runtime query. Required.
    readWriteConnectionStringSecretRef:
      name: kagent-postgres-readwrite
      key: readWriteConnectionString
    # Project the kagent_readwrite_user Pod Certificate, and kagent_owner_user unless
    # config.database.skipMigrations is true, at /run/postgres.podcert.ate.dev.
    # Only for databases that trust the Substrate postgres CA.
    clientCertificate:
      enabled: false
config:
  database:
    owner:
      role: kagent_owner
    readWrite:
      role: kagent_readwrite
    # Schema for Kagent tables. It must exist, or the owner role must be able to create it.
    schema: kagent
    vectorSchema: extensions
    vectorEnabled: false
```

#### Preparing your own database

Run [`go/core/pkg/postgressetup/setup.sql`](../go/core/pkg/postgressetup/setup.sql) with `psql` as a database administrator. `kagent install` runs the same script against its development database. The script creates:

- a `NOLOGIN` owner role, which owns the schema and every object Kagent's migrations create;
- a `NOLOGIN` read/write role, with `USAGE` on the schema and, through the owner's default privileges, `SELECT`, `INSERT`, `UPDATE` and `DELETE` on tables, `USAGE`, `SELECT` and `UPDATE` on sequences, `EXECUTE` on routines and `USAGE` on types. It cannot change the schema;
- a `NOINHERIT` login user for each role. The logins have no privileges of their own, and Kagent assumes each role after connecting;
- the schema, owned by the owner role, with all privileges revoked from `PUBLIC`.

The script also revokes `CREATE` on the `public` schema from `PUBLIC` in the whole database. It fails rather than change an existing role with different attributes or a schema owned by another role, and running it again is safe.

Every variable is required, and the script must run in one transaction. Pass the passwords through environment variables, which `\getenv` (psql 15 or later) reads, so they stay out of the process list:

```bash
export KAGENT_OWNER_PASSWORD=... KAGENT_READWRITE_PASSWORD=...
psql "$ADMIN_CONNECTION_STRING" --no-psqlrc --set=ON_ERROR_STOP=1 --single-transaction \
  --set=kagent_schema=kagent \
  --set=kagent_owner_role=kagent_owner \
  --set=kagent_owner_user=kagent_owner_user \
  --set=kagent_readwrite_role=kagent_readwrite \
  --set=kagent_readwrite_user=kagent_readwrite_user \
  --command='\getenv kagent_owner_password KAGENT_OWNER_PASSWORD' \
  --command='\getenv kagent_readwrite_password KAGENT_READWRITE_PASSWORD' \
  --file=go/core/pkg/postgressetup/setup.sql
```

The script doesn't print the passwords, but they reach the server as statement text, so a server that logs every statement (`log_statement = all`) records them. To avoid that, pass a SCRAM-SHA-256 verifier instead of the plain password; PostgreSQL stores it as given.

| Variable | `kagent install` value |
| --- | --- |
| `kagent_schema` | `kagent` |
| `kagent_owner_role` | `kagent_owner` |
| `kagent_owner_user` | `kagent_owner_user` |
| `kagent_owner_password` | Empty (certificate authentication) |
| `kagent_readwrite_role` | `kagent_readwrite` |
| `kagent_readwrite_user` | `kagent_readwrite_user` |
| `kagent_readwrite_password` | Empty (certificate authentication) |

Each run sets each login's password to its variable. An empty value removes the password, so pass the current passwords every time. An unset variable stops the script before it changes anything.

Set `config.database.owner.role`, `config.database.readWrite.role` and `config.database.schema` to the values you used, and put a connection string for each login in the Secrets that `database.postgres.ownerConnectionStringSecretRef` and `database.postgres.readWriteConnectionStringSecretRef` name. Leave role settings out of the connection strings: Kagent assumes each role after it connects. With `config.database.skipMigrations: true`, only the read/write Secret and role are needed.

Substrate needs its own identities. Run Substrate's [`pkg/postgressetup/setup.sql`](https://github.com/kagent-dev/substrate/blob/v0.6.0-alpha3/pkg/postgressetup/setup.sql) the same way, with its `substrate_*` variables.

When vectors are enabled, install pgvector into `config.database.vectorSchema` before Kagent starts, and grant both Kagent roles `USAGE` on that schema. Migrations do not create the extension, and startup fails if it is installed in a different schema.

The Substrate chart reads its own connection Secrets. See that chart's `postgres.ownerConnectionStringSecretRef` and `postgres.readWriteConnectionStringSecretRef` values.

#### Credential rotation

Kagent reads the connection strings from their Secrets once, at startup. After a Secret changes, restart the controller.

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
`ghcr.io/kagent-dev/substrate/ateworker-microvm:latest`. For a pinned installation,
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

`make kagent-cli-install` builds the local CLI and runs `kagent install` against the local charts. `make helm-install` installs the charts directly with Helm. That needs a prepared database, the `kagent-postgres-owner` and `kagent-postgres-readwrite` Secrets (see [PostgreSQL](#postgresql)), and a separately installed Substrate. Native gRPC, gRPC-Web, A2A, MCP, and operational HTTP endpoints share controller port `8083`.

### Using kagent cli

```bash
export OPENAI_API_KEY=your-openai-api-key
# openAI is the default; other choices include anthropic, azureOpenAI, gemini, and ollama
# config.yaml: install: {defaultModelProvider: OpenAI}
kagent --config config.yaml install
```

## Application configuration

`config` follows the [ControllerConfig schema](../proto/kagent/config/v1/config.proto).
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
