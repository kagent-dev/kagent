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

### Controller RBAC

The chart separates controller permissions into two Roles and bindings:

- `reconciliation-role` grants shared reads, status updates, and finalizer
  updates. SandboxTemplate finalizers use the main resource endpoint, so this
  Role also grants `update` on SandboxTemplates.
- `management-role` grants the management APIs' CRD and Secret/ConfigMap writes,
  plus Service deletion for tool servers. API reads use the reconciliation Role.

Both bindings are always enabled and reference the controller ServiceAccount.
The split organizes permissions; it does not provide a mode that disables
management API writes. Leader-election leases and event writes retain their
separate Role in the installation namespace.

`rbac.namespaces` (or its `global.watchNamespaces` fallback) scopes both Roles
and bindings to the selected namespaces. With no namespace restriction, the chart
creates ClusterRoles and ClusterRoleBindings; namespace discovery reads are
included only in this mode.

### PostgreSQL

The Kagent chart does not deploy or initialize PostgreSQL, and it does not install Substrate. A direct Helm install needs a prepared database, a connection Secret in the Kagent namespace, and Substrate installed as its own release.

`kagent install` does all of this for development: it installs Substrate in `ate-system`, deploys a PostgreSQL instance in the Kagent namespace, creates the identities and schemas below, writes the connection Secrets, and then installs Kagent. Kagent, Substrate, and PostgreSQL authenticate with Pod Certificates rather than passwords. The development database has no pgvector extension and is not intended for production. It pulls `postgres:18-alpine` from Docker Hub; set `install.bundledPostgresImage` in the CLI config to use a mirror.

The CLI installs `substrate-atelet` in `ate-system` and waits for it before
installing the main `substrate` release with `atelet.enabled=false`. Both charts
use the version and repository from `install.substrate` by default;
`install.atelet` can override these and supply its own `valuesFiles`. Move node
settings there when they should apply only to Atelet. Existing
`install.substrate.valuesFiles` and `values` are inherited by Atelet, including
`atelet.extraArgs`, resources, credential provider paths, image registry,
`imagePullSecrets`, `global`, `preview`, and telemetry. Atelet-specific files and
then Atelet-specific `values` override the shared settings, including shared
`--set` values. Exported installations use the same precedence.

The development install prepares the `kagent-rustfs-credentials` Secret before
Atelet starts, using `rustfs.accessKey` and `rustfs.secretKey` from the selected
Substrate chart's defaults plus your overrides. It preserves an existing Secret
and stops if its credentials differ from the configured values. With
`rustfs.enabled=false`, it skips this Secret and the RustFS endpoint defaults.
The shared `atelet.storageBackend` becomes `objectStore.backend` for Atelet.
For a different store, configure the main chart's storage settings and the
Atelet chart's `objectStore` settings. Point `objectStore.credentialsSecret` at
your existing Secret, or set it to `""` to use the SDK credential chain. See the
[Atelet chart values](https://github.com/kagent-dev/substrate/blob/v1.0.0/charts/substrate-atelet/README.md#values).

Existing installations can move Atelet to the separate release in place. Use
Helm 3.17 or later and follow [Moving atelet to substrate-atelet in the rc4
release notes](https://github.com/kagent-dev/substrate/releases/tag/v1.0.0-rc4#moving-atelet-to-substrate-atelet)
before running `kagent install` again, with these Kagent-specific adjustments:

1. Name the RustFS credentials Secret `kagent-rustfs-credentials` and set
   `objectStore.credentialsSecret` to that name in `atelet-values.yaml`. Use the
   existing Substrate release's effective RustFS keys, including any overrides.
2. Keep the upstream `helm.sh/resource-policy=keep` annotations during adoption.
   `--take-ownership` alone does not prevent the base upgrade from deleting
   Atelet. After step 3, wait for the full DaemonSet rollout before continuing:

   ```sh
   kubectl rollout status daemonset/atelet -n ate-system --timeout=5m
   ```

3. In step 4, update the WorkerPool through the Kagent release. Set
   `KAGENT_CHART_VERSION` to the currently installed Kagent chart version so this
   step changes only the worker image. Adjust the release and namespace if needed:

   ```sh
   helm upgrade kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
     --version "${KAGENT_CHART_VERSION:?Set the installed Kagent chart version}" \
     -n kagent --reuse-values \
     --set substrateWorkerPool.workerImage=ghcr.io/kagent-dev/substrate/ateworker-gvisor:v1.0.0 \
     --wait
   ```

   For microVM WorkerPools, use `ateworker-microvm:v1.0.0` instead. Update any
   other WorkerPools through their owning configuration as well.
4. In step 5, pass `-n ate-system` when upgrading `substrate-crds` installed by
   the CLI. Upgrade the base chart with `atelet.enabled=false`, then remove the
   keep annotations as described in step 6.

Runtime TaskStore calls require Actor JWTs injected by Substrate egress. Set
`config.substrate.actorJwtIssuer` to Substrate's `ateApi.actorJWTIssuer` (both
default to `https://idp.ate-system.svc`). The controller uses
`config.substrate.caFile` to verify discovery and JWKS TLS; enabling
`controller.substrateClientCertificate.enabled` supplies the Substrate ServiceDNS
trust bundle. When the issuer is served under a URL signed by a different CA,
set `controller.actorJwksCA.configMap.name` (and `.key`, default `ca.crt`) to a
ConfigMap in the Kagent namespace holding that CA in PEM form. The chart mounts
it and sets `config.substrate.actorJwksCaFile`. The controller trusts that
bundle, in addition to `caFile`, only when fetching discovery and JWKS; ate-api
and router connections never use it. The controller reads both bundles at
startup, so restart it after rotating either CA. Discovery and signing keys must
be reachable at controller startup.
Upgrade controller and runtime images together and recreate snapshots of older
runtime images that sent unsigned identity headers.

Substrate v1.0.0 restricts API ingress to namespaces labeled
`ate.dev/api-access=true`. The CLI installer applies this label to the Kagent
namespace. For direct Helm installations, label the controller namespace before
starting Kagent:

```sh
kubectl label namespace kagent ate.dev/api-access=true --overwrite
```

Upgrade Kagent and the Substrate CRDs, control plane, and workers together:
v1.0.0 uses `ate.dev/v1` Kubernetes resources and the
`substrate.ateapi.v1` gRPC API.

Substrate v1.0.0 requires mTLS and an explicit allowlist for actor ingress.
`kagent install` adds the rendered controller ServiceAccount's SPIFFE identity to
`atenetRouter.ingressAllowedSPIFFEIDs`. For direct Helm installations, set this
Substrate value to the controller identity, normally
`spiffe://cluster.local/ns/<namespace>/sa/kagent-controller`. An empty list denies
all actor traffic. Keep this identity aligned with the controller's pod certificate
when overriding its ServiceAccount or certificate trust domain.

Rc1 also changes Substrate's node-local image cache format. Stop actors on each
node before deleting its old image cache (`/var/lib/ate/image-cache` by default);
Atelet recreates it. See the [Substrate chart upgrade notes](https://github.com/kagent-dev/substrate/blob/v1.0.0/charts/substrate/README.md)
before upgrading an existing installation.

Substrate v0.7.0-alpha2 squashed its PostgreSQL migrations. Databases created by
earlier releases, including v0.7.0-alpha1, cannot be upgraded in place and must be
recreated. See Substrate's [schema evolution guide](https://github.com/kagent-dev/substrate/blob/v1.0.0/docs/dev/postgresql-schema-evolution.md).

The v0.6.0 snapshot-format transition requires a fresh development database and
recreated Actors, Tags, and WorkerPools; Substrate does not migrate the older
snapshot metadata. WorkerPools use
`spec.sandboxClasses[]`, with an existing `configRef` of the same runtime class.

Kagent configures snapshot storage per Harness or SandboxTemplate, and runtime
bootstrap requires golden egress policies. Substrate v1.0.0 requires
`preview: [PerTemplateSnapshotLocation, GoldenEgressPolicy]` in its Helm values
for these features. `kagent install` enables both gates by default; retain them
when supplying your own `preview` list or installing Substrate directly.

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
global owner access, which is required to create atespaces. It also grants
`system:serviceaccount:ate-system:ate-client` global owner access so `kubectl ate`
works with its default credentials. Anyone allowed to mint tokens for that shared
ServiceAccount receives its access. Direct Helm installs
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

Substrate needs its own identities. Run Substrate's [`pkg/postgressetup/setup.sql`](https://github.com/kagent-dev/substrate/blob/v1.0.0/pkg/postgressetup/setup.sql) the same way, with its `substrate_*` variables.

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

#### Default Harnesses

`kagent install` creates the WorkerPool and three Harnesses in the release
namespace, so an Agent there can reference one without writing its own:

| Harness | Runtime |
| --- | --- |
| `kagent-default` | kagent |
| `codex-default` | Codex |
| `claude-default` | Claude |

Direct Helm installations opt in with `harnesses.enabled=true`:

```yaml
substrateWorkerPool:
  create: true
  workerImage: <worker-image>
harnesses:
  enabled: true
  # Substrate snapshot storage. The default uses the bucket of Substrate's bundled RustFS.
  snapshotLocation: s3://ate-snapshots/kagent/
  # Skip an individual runtime:
  claude:
    enabled: false
```

Each default Harness references `substrateWorkerPool.name` and omits
`workload`, so it runs the release's built-in image for its runtime
(`config.harnessImages`). Agent `spec.harnessRef` is namespace-local; Agents in
other namespaces need their own Harness.

The default OpenAI ModelConfig sets `apiFormat: responses`, which the Codex
runtime requires and the kagent runtime supports.

### Using Make

```bash
export OPENAI_API_KEY=your-openai-api-key

# Build the local images, then install them with Substrate and the development database
make build
make MODEL_PROVIDER=openAI kagent-cli-install
```

`make kagent-cli-install` builds the local CLI and runs `kagent install` against the local charts. `make helm-install` installs the charts directly with Helm. That needs a prepared database, the `kagent-postgres-owner` and `kagent-postgres-readwrite` Secrets (see [PostgreSQL](#postgresql)), and a separately installed Substrate. Native gRPC, gRPC-Web, A2A, MCP, and operational HTTP endpoints share controller port `8083`.

Use `kagent install --dry-run --output-dir ./kagent-install` to export the Helm commands and merged values without changing the cluster. See [Exporting an installation](../docs/installation.md) for credential handling and prerequisite setup.

### Using kagent cli

```bash
export OPENAI_API_KEY=your-openai-api-key
# OpenAI is the default; --provider Anthropic (with ANTHROPIC_API_KEY) overrides
# install.defaultModelProvider for one install.
# config.yaml: install: {defaultModelProvider: OpenAI}
kagent --config config.yaml install
```

To install the CLI first, run `curl -sL https://kagent.dev/install | bash`. It
installs the newest final release that publishes a binary for your platform;
pass `--prerelease` to include release candidates or `--version vX.Y.Z` to pin
one.

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
