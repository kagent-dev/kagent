# Development

CLI and controller settings use the protobuf schemas in
`proto/kagent/config/v1/config.proto` and Protovalidate. ADK runtime settings use
`proto/kagent/config/v1/adk.proto`; other harnesses use their native config structs.
See [process configuration](docs/architecture/process-configuration.md)
for file locations, precedence, and examples.

Standard `OTEL_*` settings override the receiving process's telemetry defaults.
Provider credentials, SDK trust, `KUBECONFIG`, UI settings, and test inputs remain
environment interfaces. Register supported public variables in `go/core/pkg/env`,
then run `make env-docs` and `make env-docs-check`. Generated runtime payloads,
credentials, and private bootstrap paths are excluded from this reference.

To understand how to develop for kagent, it's important to understand the architecture of the project. Please refer to the [README.md](README.md#architecture) file for an overview of the project.

When making changes to `kagent`, the most important thing is to figure out which piece of the project is affected by the change, and then make the change in the appropriate folder. Each piece of the project has its own README with more information about how to setup the development environment and run that piece of the project.

- [python](python): Contains the code for the ADK engine.
- [go](go): Contains the code for the kubernetes controller, and the CLI.
- [ui](ui): Contains the code for the web UI.

## Releases

Run the [Release workflow](https://github.com/kagent-dev/kagent/actions/workflows/tag.yaml)
manually, selecting the branch to build and entering the new release tag
(for example, `v0.1.0` or `v0.1.0-rc.1`). The `v` prefix is required.

```shell
gh workflow run tag.yaml --ref main -f version=v0.1.0
```

The workflow builds the selected branch's commit and publishes images, Helm charts,
Python packages, and release artifacts using that version. After those jobs succeed,
it creates the `v<version>` tag at the same commit and publishes the GitHub release
with generated notes and artifacts. Existing tags are rejected before publishing;
pushing a tag no longer starts a release.

## Nightly releases

The [Nightly Release workflow](https://github.com/kagent-dev/kagent/actions/workflows/nightly.yaml)
runs at 02:00 UTC on the default branch, skipping builds when its commit matches
the previous successful nightly. Maintainers can also run it manually on the
default branch to force a rebuild.

Use **Run workflow** on that page, or run
`gh workflow run nightly.yaml --ref main`. Both nightly and manual releases use
the shared `publish-image`, `publish-helm`, and `build-release-artifacts` composite
actions in `.github/actions`.

Each build publishes all six component images and the `kagent` and `kagent-crds`
Helm charts as `0.0.0-alpha.g<12-character-commit>`. These are development builds;
nightly runs do not publish Python packages to PyPI or create GitHub releases.

To install a nightly, use the version from its workflow summary for both charts
with your usual installation values. Replace the example version below:

```shell
NIGHTLY_VERSION=0.0.0-alpha.g0123456789ab
helm upgrade --install kagent-crds oci://ghcr.io/kagent-dev/kagent/helm/kagent-crds \
  --version "$NIGHTLY_VERSION" --namespace kagent --create-namespace
helm upgrade --install kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
  --version "$NIGHTLY_VERSION" --namespace kagent -f your-values.yaml
```

Each workflow run includes a changelog in its summary and an artifact containing
CLI binaries, checksums, and commit-specific chart archives.

## Prerequisites

- **Docker** with **Docker Buildx** (v0.23.0+)
- **Git**
- **Go** (v1.27.0+)
- **Helm**
- **Kind** (v0.27.0+)
- **kubectl** (v1.33.4+)
- **Make**
- **Node.js** (v24.13.0+) with **Corepack/Yarn**
- **uv** (v0.10.4+)

## How to run everything in Kubernetes

1. Create a Kind cluster:

```shell
make create-kind-cluster
```

2. Set your model provider:

```shell
export MODEL_PROVIDER=openAI
#or
export MODEL_PROVIDER=anthropic
```

3. Set the provider's API key:

```shell
export OPENAI_API_KEY=your-openai-api-key
#or
export ANTHROPIC_API_KEY=your-anthropic-api-key
```

   Alternatively, create a `.env` file at the repo root (gitignored). This file
   is loaded by the Makefile (via `-include .env`) to inject environment
   variables when you run `make` targets:

```shell
# Set your provider (supported: openAI, anthropic, azureOpenAI, gemini, ollama)
MODEL_PROVIDER=openAI

# Set the corresponding API key for your provider
OPENAI_API_KEY=your-openai-api-key
# ANTHROPIC_API_KEY=your-anthropic-api-key
# GOOGLE_API_KEY=your-google-api-key
```

4. Build and push the local Kagent images to the Kind registry:

```shell
make build
```

5. Install the local Kagent chart and its development dependencies:

```shell
make kagent-cli-install
```

`kagent-cli-install` builds the local CLI, packages the local Kagent charts, and
runs `kagent install`. The Make target selects the Kind context, and the CLI then:

1. Installs the Kagent and Substrate CRDs.
2. Creates the certificate-authority material required by Substrate.
3. Installs the Substrate PodCertificate controller.
4. Creates a development PostgreSQL deployment and initializes the Kagent and
   Substrate schemas and roles.
5. Installs Substrate and Kagent with certificate-authenticated connections to
   that PostgreSQL deployment.

The command opens the Kagent dashboard after the installation completes.

To apply personal Helm overrides without committing them, set
`HELM_ARGS` in your `.env` file. The variable accepts one or more
Helm `--set` overrides:

```shell
# .env
HELM_ARGS=--set ui.replicas=0 --set config.logging.level=debug
```

The Substrate chart version defaults to the `github.com/kagent-dev/substrate`
version in `go/go.mod`. The following Make variables select other chart sources
for local or prerelease testing:

```shell
make kagent-cli-install \
  SUBSTRATE_REPO=oci://example.com/substrate/helm \
  SUBSTRATE_VERSION=0.5.0-alpha2
```

The PodCertificate chart inherits those values. Override it independently when
the chart is published at another location or version:

```shell
make kagent-cli-install \
  SUBSTRATE_PODCERT_REPO=oci://example.com/podcert/helm \
  SUBSTRATE_PODCERT_VERSION=0.5.1
```

For direct CLI use, configure `install.substrate` and `install.podCertificate`
in the [CLI config file](docs/architecture/process-configuration.md#cli).

To access the UI, port-forward to the UI port on the `kagent-ui` service:

```shell
kubectl port-forward svc/kagent-ui 8001:8080
```

Then open your browser and go to `http://localhost:8001`.

### Addons

Optional addons are available to enhance your development environment with
observability and infrastructure components.

**Prerequisites:** Complete the cluster, provider, and installation steps above.

To install all addons:

```shell
make kagent-addon-install
```

This installs the following components into your cluster:

| Addon          | Description                          | Namespace      |
|----------------|--------------------------------------|----------------|
| Istio          | Service mesh (demo profile)          | `istio-system` |
| Grafana        | Dashboards and visualization         | `kagent`       |
| Prometheus     | Metrics collection                   | `kagent`       |
| Metrics Server | Kubernetes resource metrics          | `kube-system`  |

`make kagent-cli-install` deploys PostgreSQL as development infrastructure before
installing Kagent and Substrate. PostgreSQL is not part of the Kagent Helm chart.
A direct Helm installation requires a prepared database (see
[Preparing your own database](helm/README.md#preparing-your-own-database)), the
`kagent-postgres-owner` and `kagent-postgres-readwrite` connection Secrets in the
Kagent namespace, and a separately installed Substrate.
The optional addons above provide observability components.

> **pgvector:** The PostgreSQL image `kagent install` deploys does not include the pgvector
> extension. Vector features require a PostgreSQL deployment with pgvector and
> `config.database.vectorEnabled=true`.

Verify the database connection by checking the controller logs:

```shell
kubectl logs -n kagent deployment/kagent-controller | grep -i postgres
```

### Troubleshooting

### buildx localhost access

The `make build` command might time out with an error similar to the following:

> ERROR: failed to solve: DeadlineExceeded: failed to push localhost:5001/kagent-dev/kagent/controller

As part of the build process, the `buildx` container tries to build and push the kagent images to the local Docker registry. The `buildx` command requires access to your host machine's Docker daemon.

Recreate the buildx builder with host networking, such as with the following example commands. Update the version and platform accordingly.

```shell
docker buildx rm kagent-builder-v0.23.0

docker buildx create --name kagent-builder-v0.23.0 --platform linux/amd64,linux/arm64 --driver docker-container --use --driver-opt network=host
```

Then run `make build` and `make kagent-cli-install` again.

### Run kagent and an agent locally

Create a Kind cluster and install the development stack. After the dashboard
opens, stop its port-forward with Ctrl-C and scale Kagent to zero replicas so it
can be run locally.

```bash
make create-kind-cluster
make build
make kagent-cli-install
kubectl scale -n kagent deployment kagent-controller --replicas 0
```

For a standalone agent, mount a root runtime document at `/config/config.json`:

```json
{"runtime":{"control_plane_url":"http://localhost:8083","gateway_url":"http://localhost:8083","name":"kebab-agent","namespace":"kagent"}}
```

```bash
docker run --rm --net=host -v "$PWD/config.json:/config/config.json:ro" localhost:5001/kebab:latest
```

The control plane must be reachable from the agent process. Managed agents receive
these process settings from their Harness compiler.

## Telemetry

The telemetry contract is a Weaver registry in `telemetry/registry`. The Go
constants in `go/pkg/telemetry/conv`, the Python constants in
`kagent.core.telemetry.conv`, and `docs/architecture/telemetry-contract.md` are
generated from it. Never edit them by hand. After a registry change, run:

```bash
make semconv-generate   # regenerate everything from the registry
make semconv-verify     # what CI runs: check, policy tests, generate, drift check
```

These targets need either Docker or a local `weaver` of exactly the version in
`telemetry/versions.env`. A different local version is refused, so a green run
on a laptop means the same as in CI. See
[docs/architecture/telemetry.md](docs/architecture/telemetry.md) for the contract.

`telemetry/live-check.sh start` and `stop` run Weaver live-check around an e2e
run; see the Conformance section of that document.

To look at traces locally, `make otel-local` starts Jaeger with an OTLP receiver
on ports 4317 and 4318 and its UI on http://localhost:16686. Point an install
at it with `--set config.runtimeDefaults.telemetry.traces.endpoint=http://<host>:4317`.
`make otel-collector-kind` installs the reference collector from
`examples/observability` into the kind cluster and prints what it receives.
