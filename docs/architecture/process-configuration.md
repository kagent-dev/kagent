# Process configuration

The CLI and controller read YAML or JSON documents described by
[`config.proto`](../../proto/kagent/api/v1alpha1/config.proto). Protovalidate
annotations define valid values and cross-field constraints. Files have no
`apiVersion` or `kind`: the executable selects its schema.

## CLI

The default file is `kagent/config.yaml` under the platform's user configuration
directory (`$XDG_CONFIG_HOME`, or `~/.config`, on Linux). A missing default file
uses built-in defaults. An explicitly supplied `--config` file must exist.

Precedence is built-in defaults, file, then explicitly supplied flags. Existing
connection and output flags remain useful for one command. Relative file paths
in a document resolve against its directory; flag paths resolve against the
working directory.

```yaml
connection:
  apiUrl: http://localhost:8083
  gatewayUrl: http://localhost:8083
  namespace: kagent
  userId: admin@kagent.dev
  timeout: 300s
output:
  format: table
logging:
  level: info
install:
  defaultModelProvider: OpenAI
  kagent:
    valuesFiles: [kagent-values.yaml]
database:
  urlFile: secrets/postgres-url
```

```bash
kagent --config config.yaml config validate
kagent --config config.yaml config view
kagent config schema --component controller
```

`schema` emits a protobuf descriptor set with validation annotations. `view`
prints resolved settings and file references, never secret file contents. These
commands make no service connections. Database credentials are opened only by
database commands; `--db-url-file` overrides the configured reference.

`install.bundledPostgresImage` selects the development PostgreSQL image, including
mirrors.

Installation passes embedded defaults before configured chart values files.
Files are applied in order, with the last file winning. Provider credentials
continue to use the provider SDK environment interface.

## Controller

The controller reads `/etc/kagent/controller.yaml`, or the file selected by
`--config`. `--log-level` overrides its configured log level.
`--check-config` validates the document and required secret files without
starting the controller.

```yaml
kubernetes:
  namespace: kagent
  leaderElection: true
server:
  bindAddress: :8083
  auth:
    mode: insecure
database:
  urlFile: secrets/database-url
  role: kagent_owner
  schema: kagent
  vectorSchema: extensions
  skipMigrations: false
gateway:
  publicUrl: https://kagent.example.com
  push:
    signingPrivateKeyFile: secrets/push-private-key.pem
sessions:
  idleTtl: 604800s
runtimeDefaults:
  controlPlaneUrl: http://kagent-controller.kagent.svc:8083
  gatewayUrl: http://kagent-controller.kagent.svc:8083
  logging:
    level: info
```

The controller applies database migrations at startup by default. Set
`database.skipMigrations: true` when migrations are applied out-of-band. Startup
still verifies that all required migrations are applied and fails if any are missing.

Push JWT signing is optional: `gateway.push.signingPrivateKeyFile` names an
unencrypted PKCS#8 Ed25519 PEM key when enabled. Helm projects the operator-managed
Secret selected by `controller.push.signing.secretName`.

Durations use protobuf JSON notation: `60s`, `0.5s`, or `0s`. Unknown fields,
duplicate keys, nulls, YAML aliases/merges, and multiple documents are rejected.
Defaults preserve explicit false and zero values. Zero disables session idle
expiration and leaves share TTL unbounded; nonzero intervals must be positive.

Helm's top-level `config` block mirrors the schema and includes both controller
settings and managed runtime defaults. The chart writes one config file,
projects database and signing secrets separately, and changes the pod checksum
when settings change. Deployment settings (images, probes, volumes, Services,
Secret references) remain Helm values outside `config`.

The controller loads and validates once, then passes typed settings to owners.
Reconciliation and request handling do not read application settings from env.
Configuration changes require a process restart.

## Runtimes

Runtime configuration remains the existing Go/JSON contract. ADK and BYO
process settings live in the root `runtime` object; subagents inherit them.
Native Claude and Codex retain their harness-specific Go structs. The controller
compiles `runtimeDefaults` into these payloads independently of its own settings.

```json
{
  "runtime": {
    "control_plane_url": "http://kagent-controller.kagent.svc:8083",
    "gateway_url": "http://kagent-controller.kagent.svc:8083",
    "name": "researcher",
    "namespace": "kagent",
    "log_level": "info",
    "port": "80",
    "max_content_length": 0,
    "file_search_tools": false,
    "token_propagation": {"enabled": false}
  }
}
```

An absent request size limit uses the runtime default; zero means unlimited.
Python integrations load this document with `KAgentConfig.from_file()` or receive
an explicit `RuntimeConfig`. Managed processes still materialize the private
`KAGENT_CONFIG_JSON` and `KAGENT_AGENT_CARD_JSON` payloads supplied by Substrate.
Actor/session identity, credential projection, and private bootstrap paths keep
their existing contracts. Substrate's limits remain 32 environment entries and
32,768 characters per value.

## OpenTelemetry

File settings provide telemetry defaults; a process's standard `OTEL_*` inputs
override them. Signal-specific env values win over generic OTLP env values, and
both win over file defaults. An absent exporter stays off unless env explicitly
enables it. `OTEL_SDK_DISABLED=true` disables all SDK export.
An endpoint or protocol override replaces the file's entire connection pair;
provide both to avoid using an SDK default for the missing counterpart.

Controller overrides apply only to the controller. Managed runtimes use
`runtimeDefaults.telemetry` plus their own `Harness.spec.env` overrides. Helm
supports `controller.env` and `controller.envFrom` for controller OTEL and
external SDK inputs. See [telemetry](telemetry.md) for exporter and capture details.

Other remaining public env interfaces are documented in [the generated
reference](../env.md): provider credentials, SDK trust, `KUBECONFIG`, UI settings,
and test inputs. Application settings have no env fallback.
