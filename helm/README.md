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
```

### Substrate PostgreSQL

Kagent supports three PostgreSQL layouts with embedded Substrate.

1. Share Kagent's bundled PostgreSQL. Kagent and Substrate use the same
   database with separate schemas; the parent chart creates Substrate's
   release-scoped connection Secret.

```yaml
substrate:
  enabled: true
```

2. Share one external Secret. Helm cannot dynamically copy a parent Secret
   reference into a dependency, so repeat the same name and key explicitly.

```yaml
database:
  postgres:
    secretRef:
      name: shared-postgres
      key: connectionString
    bundled:
      enabled: false
substrate:
  enabled: true
  postgres:
    enabled: false
    connectionStringSecretRef:
      enabled: true
      name: shared-postgres
      key: connectionString
```

3. Use separate Kagent, Substrate runtime/DML, and Substrate DDL/maintenance
   Secrets.

```yaml
database:
  postgres:
    secretRef:
      name: kagent-postgres
      key: connectionString
    role: kagent_app
    bundled:
      enabled: false
substrate:
  enabled: true
  postgres:
    enabled: false
    schema: substrate
    connectionStringSecretRef:
      enabled: true
      name: substrate-runtime-postgres
      key: connectionString
    ddlConnectionStringSecretRef:
      enabled: true
      name: substrate-ddl-postgres
      key: connectionString
    runtimeRole: substrate_runtime
    ddlRole: substrate_ddl
```

The DDL role owns the Substrate schema and performs migrations and partition
maintenance. Substrate grants its runtime role access to migrated tables and
sequences. Omitting the DDL connection preserves single-connection operation.

An inline `database.postgres.url` remains supported, but is fixed for the life
of the controller process. When embedded Substrate is enabled, the parent chart
can copy that inline value into its release-scoped Substrate Secret.

#### Credential rotation

`database.postgres.secretRef` is mounted through a Secret volume. Kagent
rereads the connection string before opening each new physical connection;
existing sessions remain valid until pgx retires them. Set
`database.postgres.pool.maxConnLifetime` to bound Kagent's turnover time. When
embedded Substrate shares the Secret, set
`substrate.postgres.pool.maxConnLifetime` as well to bound its runtime, watch,
and DDL pools. Keep old and new credentials valid long enough for Kubernetes
Secret projection and connection turnover.

Rotation may change passwords, usernames, and referenced TLS material. For a
username-changing rotation, set `database.postgres.role` to a stable `NOLOGIN`
role and grant every incoming login membership before publishing the Secret.
Set `substrate.postgres.runtimeRole` and `substrate.postgres.ddlRole` the same
way for embedded Substrate. Neither Kagent nor either Helm chart creates these
roles or grants membership: database provisioning must create the roles before
installation, and the credential rotator must grant each incoming login before
publishing its Secret. Without stable roles, changing a username requires a
restart. Host, port, fallback targets, and database always require a restart.
Direct binary deployments use
`POSTGRES_DATABASE_URL=@file:/absolute/path`; there is no separate `_FILE`
environment variable.

Kagent 1.x removes `database.postgres.urlFile`. Replace:

```yaml
database:
  postgres:
    urlFile: /user-managed/path
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
