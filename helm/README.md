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

The default install uses one PostgreSQL instance and one `kagent` database.
Kagent uses the `kagent` schema by default. Substrate uses the `substrate` schema.
This identity layout requires a fresh database; upgrading an existing database to it is unsupported.
When vectors are enabled, `database.postgres.vectorSchema` names the one schema
that holds the shared pgvector extension (default `extensions`). All Kagent installs
using the same database must select that schema. For an external database,
install pgvector there before running migrations, or set `vectorSchema` to its
existing location (such as `public`). Grant the application role
`USAGE` on the extension schema. Set `POSTGRES_VECTOR_SCHEMA` to the same value
when running the database CLI outside the chart.
When separate from Kagent's table schema, the pgvector schema stays out of its
normal SQL search path. Kagent qualifies its pgvector type, index operator
class, and cosine operator references. A shared `extensions` schema may also
contain other applications' objects. Grant Kagent
`USAGE` on that schema; reserve `CREATE` for trusted administrators.

With `database.postgres.bundled.bootstrap=true`, each controller pod runs identity
bootstrap on every start, before migrations. Repeated runs create only missing
identities and do not reset existing passwords. If `database.postgres.vectorEnabled`
changes from `false` to `true`, the next start installs the `vector` extension in
`vectorSchema` and then applies pending vector migrations. The default bundled
PostgreSQL image does not include pgvector; select an image with pgvector installed
before enabling vectors. With bootstrap disabled or an external database, install
the extension yourself before enabling vectors.

The install creates separate users and group roles:

| Product access | User | Group role |
| --- | --- | --- |
| Kagent | `kagent_user` | `kagent_owner` |
| Substrate owner | `substrate_admin_user` | `substrate_owner` |
| Substrate read/write | `substrate_readwrite_user` | `substrate_readwrite` |

Enable Substrate to use this layout:

```yaml
substrate:
  enabled: true
```

The chart creates `postgres-admin` for the bundled database. Each control plane uses this Secret before it runs migrations.
If you supply another administrator Secret, set both `database.postgres.bundled.adminSecretRef` and `substrate.postgres.adminSecretRef` to the same name and keys.

The chart also creates three application Secrets. Its default administrator and application passwords are fixed, published values. This bundled bootstrap setup is for development and evaluation, not production. For production, provision unique users and permissions externally, provide connection Secrets, and disable bootstrap. For Substrate, Kagent passes the bundled PostgreSQL Service address and the `kagent` database name to connection-string templates owned by the Substrate chart. Those templates supply Substrate's fixed usernames and passwords.

For an external database, create the users, roles, schemas, and grants yourself, then provide three application connection Secrets:

```yaml
database:
  postgres:
    secretRef:
      name: kagent-postgres
      key: connectionString
    bundled:
      enabled: false
substrate:
  enabled: true
  postgres:
    enabled: false
    readWriteConnectionStringSecretRef:
      name: substrate-postgres-readwrite
      key: readWriteConnectionString
    ownerConnectionStringSecretRef:
      name: substrate-postgres-owner
      key: ownerConnectionString
    bootstrap: false
```

Bundled bootstrap creates only the fixed users, using the same fixed development credentials compiled into each product and rendered into its connection Secrets. It also creates group roles, schemas, memberships, and grants. It does not change existing passwords. Bootstrap rejects a connection Secret whose credentials differ from those fixed defaults.

To use externally created users with the bundled database, first create the replacement users and connection Secrets. Then set `database.postgres.bundled.bootstrap=false` and provide `database.postgres.secretRef.name` in the same upgrade. If Substrate is enabled, set `substrate.postgres.bootstrap=false` and provide its owner and read/write Secret references too. The bundled PostgreSQL pod still uses its administrator Secret; neither control plane resets the original users' passwords.

For a BYO database, create these objects before installation. Keep migrations enabled.
Set `database.postgres.role` to the Kagent owner role and, when Substrate is
enabled, set `substrate.postgres.ownerRole` and `substrate.postgres.readWriteRole`
to the roles you provisioned. Use distinct role names and table schemas for
separate installs sharing one database. Give each install separate logins and
grant each login membership only in its install's roles. Bundled bootstrap uses
the fixed role names and requires the default values.

The application uses the same identity SQL that operators can run: [Kagent identity SQL](../go/core/pkg/migrations/identity/bootstrap.sql) and `cmd/ateapi/internal/store/atepg/identity.sql` in the Substrate repository. These files sit beside the migration sources but run separately, as an administrator. Set the transaction-local parameters listed at the top of each file before running it.
For manual provisioning with custom chart role names, set
`kagent.bootstrap_owner_role`, `substrate.bootstrap_owner_role`, and
`substrate.bootstrap_readwrite_role` as transaction-local settings before
running the applicable SQL file. They default to the fixed development names;
the bundled binary bootstrap passes those fixed names explicitly.

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
