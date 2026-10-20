# Process configuration

The CLI and controller read YAML or JSON documents described by
[`config.proto`](../../proto/kagent/config/v1/config.proto). Protovalidate
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
  owner:
    urlFile: secrets/postgres-owner-url
    role: kagent_owner
```

```bash
kagent --config config.yaml config validate
kagent --config config.yaml config view
kagent config schema --component controller
```

`schema` emits a protobuf descriptor set with validation annotations. `view`
prints resolved settings and file references, never secret file contents. These
commands make no service connections. Database credentials are opened only by
database commands, which connect as `database.owner`; `--db-url-file` and
`--db-role` override its configured values.

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
  owner:
    urlFile: secrets/database-owner-url
    role: kagent_owner
  readWrite:
    urlFile: secrets/database-readwrite-url
    role: kagent_readwrite
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

The controller connects with two logins. `database.owner` applies migrations at
startup; `database.readWrite` serves every runtime query. Each assumes its `role`
after connecting. Set `database.skipMigrations: true` when migrations are applied
out-of-band; the owner login is then not needed, and startup still verifies,
through the read/write login, that all required migrations are applied.

The controller owns callback delivery through the top-level `push` section
(`config.push` in Helm). Push JWT signing is optional:
`push.signingPrivateKeyFile` names an
unencrypted PKCS#8 Ed25519 PEM key when enabled. Helm projects the operator-managed
Secret selected by `controller.push.signing.secretName`.

Config files use `sigs.k8s.io/yaml.YAMLToJSONStrict` followed by Go's
`protojson.Unmarshal`. Protovalidate checks the schema after command-line overrides
and application defaults are applied. Parsing follows those libraries:

- Duplicate keys and unknown protobuf fields are rejected. Supplying both the
  camelCase and snake_case names of the same protobuf field is also rejected.
- YAML 1.1 scalar rules apply: unquoted `yes`, `no`, `on`, and `off` are booleans;
  quote them when a string is intended. YAML numeric forms such as `0x10` are
  converted to numbers, and numeric mapping keys become strings.
- YAML anchors, aliases, and merge keys are resolved by the YAML library. Strict
  mode rejects duplicate keys, including an explicit override of a merged key.
- Only the first YAML document is read; subsequent documents are ignored.
- ProtoJSON accepts quoted integers for integer fields, but a quoted boolean is
  a string and fails boolean decoding. Durations use `60s`, `0.5s`, or `0s`.
- Field `null` means unset; it does not disable a setting. Null list elements are
  rejected. An explicitly empty string is validated against the field's schema.

Defaults follow protobuf presence. Ordinary scalars use their protobuf zero value;
for example, omitted and explicit zero `database.pool.minConns` both mean zero.
Optional scalars and message fields distinguish omission from an explicit value:
`kubernetes.leaderElection` defaults to true but accepts false, `sessions.idleTtl`
defaults to seven days but `0s` disables expiration, and omitted
`runtimeDefaults.maxContentLength` selects the runtime limit while zero disables
that limit. Session shares have no TTL bound by default; nonzero intervals must
be positive.

Helm's top-level `config` block mirrors the schema and includes both controller
settings and managed runtime defaults. The chart writes one config file,
projects database and signing secrets separately, and changes the pod checksum
when settings change. Deployment settings (images, probes, volumes, Services,
Secret references) remain Helm values outside `config`.

Release charts populate `harnessImages` from their packaged image catalog.
`config.harnessImages` can override individual defaults; keys are
`release`, `kagent`, `claude`, and `codex`. An empty image disables that default.
The controller checks a nonempty catalog release against its own version.
See [Harness image selection](configuration-and-compilation.md#harness-images-and-workload-overrides)
for compilation and upgrade behavior.

The controller loads and validates once, then passes typed settings to owners.
Reconciliation and request handling do not read application settings from env.
Configuration changes require a process restart.

## Runtimes

ADK and BYO configuration uses `ADKConfig` from
[`adk.proto`](../../proto/kagent/config/v1/adk.proto) in `kagent.config.v1`, serialized as ProtoJSON
in `config.json`. Go and Python consume generated messages and enforce the
same Protovalidate rules. Agent, model, tool, memory, compaction, plugin, and
process settings have one schema. Process settings live in the root `runtime`
object. Agent behavior lives under `agent`, whose recursive `sub_agents`
contain only `ADKAgentConfig` messages. Runtime fields cannot be set on any
agent node; the entire tree shares the top-level process settings.
Go callers use the generated types from `go/api/gen/kagent/config/v1` directly.
`go/adk/pkg/config` owns validated ProtoJSON encoding and decoding, runtime
defaults, and model and plugin conversions shared by the compiler and runtime.
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
    "port": 80,
    "max_content_length": "0",
    "builtin_tools": { "disable_file_search": false },
    "token_propagation": { "enabled": false }
  }
}
```

Models select exactly one provider, for example:

```json
{ "agent": { "model": { "model": "gpt-4o", "openai": { "temperature": 0 } } } }
```

Provider-specific fields live inside the selected provider; shared transport
settings remain beside `model`. ProtoJSON accepts snake_case and camelCase
field names and emits 64-bit integers as strings. Optional fields preserve an
explicit false or zero. Listener ports use `uint32` (1–65535), so they emit JSON
numbers; absent ports retain the runtime default. Body limits remain `int64` to
match the HTTP API, with zero meaning unlimited. Model and MCP timeouts use
protobuf durations (for example, `"0.250s"`), preserving fractional seconds.
Durations must be positive and fit Go `time.Duration`; count fields are bounded
where downstream SDKs require narrower integers. Kagent-owned model counts and
seeds retain `int64` through the Go SDK boundary. Structured output carries canonical schema bytes as
base64 so its SHA-256 still identifies the exact bytes. Bedrock
`additional_model_request_fields` also carries raw JSON bytes as base64, preserving
provider integer values without conversion to floating point.

This is a complete cutover from the previous `model.type` JSON discriminator;
runtime images and controller payloads must use the same contract. BYO images
using the kagent ADK must use the Go or Python ADK from the same release (or
source revision for development builds) as the controller. Rebuild these images
when updating the controller; mixed controller/ADK versions are not supported. Unknown
fields and multiple model providers fail configuration loading. Telemetry uses
the same `TelemetryConfig` as controller settings (including protobuf durations).

An absent request size limit uses the runtime default; zero means unlimited.

Built-in tool settings live under `runtime.builtin_tools`:
`disable_file_search`, `bash_venv_path`, and `disable_bash`. This replaces the
former `runtime.skill_tools` block (Helm: `config.runtimeDefaults.builtinTools`).
Replace the former `file_search_tools` flag with `disable_file_search`, reversing
its value: `file_search_tools: true` becomes `disable_file_search: false`.
Go and Python ADK register `read_file`, `write_file`, `edit_file`, `list_files`,
`grep_file`, and `bash` for the root agent and subagents, even when no skills are
configured or installed.
Set `runtime.builtin_tools.disable_bash` to `true` to omit built-in bash.
Set `runtime.builtin_tools.disable_file_search` to `true` to omit `list_files`
and `grep_file`. Both opt-outs default to `false`. Skill discovery and loading
remain conditional on installed skills.
For controller-managed runtimes, set
`config.runtimeDefaults.builtinTools.disableBash: true` in Helm values.
`bash_venv_path` selects a separate environment for shell commands in both runtimes:
its `bin` directory is prepended to `PATH`, and `VIRTUAL_ENV` is set.

Python integrations load this document with `KAgentConfig.from_file()` or receive
an explicit generated `RuntimeConfig`. Python ADK loads the full document with
`parse_config()` and constructs the SDK agent with `create_agent()`. Managed processes still materialize the private
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
