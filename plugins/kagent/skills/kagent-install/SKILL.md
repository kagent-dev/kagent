---
name: kagent-install
description: >-
  Install, upgrade, export and uninstall kagent on a Kubernetes cluster with
  the kagent CLI or the Helm charts: model provider keys, the PostgreSQL and
  Substrate prerequisites, dry-run export of Helm commands, and CRD-first
  upgrades. Use when setting up kagent or debugging an install.
---

# Installing kagent

References: `helm/README.md`, `docs/installation.md`.

## CLI (recommended for development)

```bash
curl -sL https://kagent.dev/install | bash     # --version vX.Y.Z to pin, --prerelease for RCs
export OPENAI_API_KEY=...
kagent --config config.yaml install            # --provider Anthropic with ANTHROPIC_API_KEY to override
```

`kagent install` installs Substrate (`substrate-atelet`, then `substrate`) in
`ate-system`, deploys a development PostgreSQL in the kagent namespace,
creates identities, schemas and connection Secrets, then installs kagent.
That database has no pgvector and is not for production. Set
`install.bundledPostgresImage` to use a mirror of `postgres:18-alpine`.

## Export instead of applying

```bash
kagent --config config.yaml install --dry-run --output-dir ./kagent-install
# with your own database:
kagent --config config.yaml install --dry-run --skip-database-setup --output-dir ./kagent-install
export OPENAI_API_KEY=...
sh ./kagent-install/install.sh
```

The output directory must not exist. The export holds `install.sh`,
`README.md` and one values file per release; it does not provision the
database or Substrate. Review values files for secrets before committing.

## Helm directly

The chart does not deploy PostgreSQL or Substrate. You need a prepared
database, the `kagent-postgres-owner` and `kagent-postgres-readwrite`
Secrets, and Substrate installed separately.

```bash
helm install kagent-crds ./helm/kagent-crds/ --namespace kagent
helm install kagent ./helm/kagent/ --namespace kagent \
  --set providers.default=anthropic --set providers.anthropic.apiKey=$ANTHROPIC_API_KEY
# providers.default: openAI (default), anthropic, azureOpenAI, mistral, ollama
```

## Upgrade and uninstall

Upgrade CRDs first, then kagent:

```bash
helm upgrade kagent-crds ./helm/kagent-crds/ --namespace kagent
helm upgrade kagent ./helm/kagent/ --namespace kagent
```

Uninstall `kagent` first. Uninstalling `kagent-crds` deletes every kagent
custom resource in all namespaces, so confirm with the user before doing it.
Preserve existing signing pools and databases on updates.

## Ports

gRPC, gRPC-Web, A2A, MCP and operational HTTP share controller port
`8083` (service `kagent-controller`).
