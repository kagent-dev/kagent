# Development

The [environment variable reference](docs/env.md) is generated from
`go/core/pkg/env`. Register user-configurable settings there, including settings
consumed by Python, the UI, or standalone runtimes. Exclude controller-generated
payloads, credentials, private paths, and other internal process wiring.
Keep defaults and descriptions aligned with
their readers, then run `make env-docs`. Use `make env-docs-check` to run the same
freshness check as CI.

Register shared settings once, passing each consuming component to
`RegisterStringVar`, `RegisterBoolVar`, `RegisterIntVar`, or
`RegisterDurationVar`, for example:

```go
RegisterStringVar("KAGENT_LOG_LEVEL", "info", "Logging level.", ComponentController, ComponentCLI, ComponentAgentRuntime)
```

The reference lists shared settings under each component. `kagent env --component`
matches any registered component; JSON output includes a `components` array.

Kagent-owned settings use the `KAGENT_` prefix. Keep names defined by upstream
SDKs and tools, such as `OTEL_*`, provider credentials, and `KUBECONFIG`, unchanged.
The pre-release rename replaces the old unprefixed names: for example,
`LOG_LEVEL` becomes `KAGENT_LOG_LEVEL`, `HTTP_BIND_ADDRESS` becomes
`KAGENT_HTTP_BIND_ADDRESS`, and the Go ADK's `PORT` becomes `KAGENT_PORT`.
The old names are no longer read by kagent.

Both ADKs use `KAGENT_PORT` for their A2A listener; Python's former
`KAGENT_A2A_GRPC_ADDRESS` is removed. The controller sets `80`. Standalone
defaults remain `8080` for Go's shared HTTP/gRPC listener and `80` for Python's
gRPC listener; Python's HTTP port is configured separately with `--port`.

UI containers and Vite development use the same `KAGENT_*` inputs, documented in
`ui/.env.example`. UI build-time switches use `KAGENT_UI_VITE_*`; extension settings
use `KAGENT_UI_EXTENSION_*`. Other `KAGENT_*` settings are not exposed to the browser.

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

## Dependencies

Before you can run kagent in Kubernetes, you need to have the following tools installed:

### Required Dependencies

- **Kind** (v0.27.0+)
- **kubectl** (v1.33.4+)
- **Helm**
- **Go** (v1.27.0+)
- **Docker**
- **Docker Buildx** (v0.23.0+)
- **Make**

### Installation Verification

You can verify your installation by running:

```shell
# Check core dependencies
kind version
kubectl version
helm version
go version
docker version
docker buildx version
make --version
```

## How to run everything in Kubernetes

1. Create a Kind cluster:

```shell
make create-kind-cluster
```

2. Set your model provider:

```shell
export KAGENT_DEFAULT_MODEL_PROVIDER=openAI
#or
export KAGENT_DEFAULT_MODEL_PROVIDER=anthropic
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
KAGENT_DEFAULT_MODEL_PROVIDER=openAI

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
4. Creates a development PostgreSQL deployment, the Kagent and Substrate
   roles, and Substrate's schema.
5. Installs Substrate and Kagent with certificate-authenticated connections to
   that PostgreSQL deployment.

The command opens the Kagent dashboard after the installation completes.

To apply personal Helm overrides without committing them, set
`KAGENT_HELM_EXTRA_ARGS` in your `.env` file. The variable accepts one or more
Helm `--set` overrides:

```shell
# .env
KAGENT_HELM_EXTRA_ARGS=--set ui.replicas=0 --set controller.loglevel=debug
```

The Substrate chart version defaults to the `github.com/kagent-dev/substrate`
version in `go/go.mod`. The following Make variables select other chart sources
for local or prerelease testing:

```shell
make kagent-cli-install \
  SUBSTRATE_REPO=oci://example.com/substrate/helm \
  SUBSTRATE_VERSION=0.5.0-alpha1
```

The PodCertificate chart inherits those values. Override it independently when
the chart is published at another location or version:

```shell
make kagent-cli-install \
  SUBSTRATE_PODCERT_REPO=oci://example.com/podcert/helm \
  SUBSTRATE_PODCERT_VERSION=0.5.1
```

The corresponding CLI environment variables are documented in
[docs/env.md](docs/env.md).

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
A direct Helm installation requires a prepared database, a `kagent-postgres`
connection Secret in the Kagent namespace, and a separately installed Substrate.
The optional addons above provide observability components.

> **pgvector:** The development PostgreSQL image does not include the pgvector
> extension. Vector features require a PostgreSQL deployment with pgvector and
> `database.postgres.vectorEnabled=true`.

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

Run kagent with `KAGENT_A2A_DEBUG_ADDR=localhost:8080` environment variable set, and when it connect to agents it will go to "localhost:8080" instead of the Kubernetes service.

Run the agent locally as well, with `--net=host` option, so it can connect to the kagent service on localhost. For example:

```bash
docker run --rm \
  -e KAGENT_API_URL=http://localhost:8083 \
  -e KAGENT_GATEWAY_URL=http://localhost:8083 \
  -e KAGENT_NAME=kebab-agent \
  -e KAGENT_NAMESPACE=kagent \
  --net=host \
  localhost:5001/kebab:latest
```

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
at it with `--set otel.traces.enabled=true --set otel.exporter.otlp.endpoint=http://<host>:4317`.
`make otel-collector-kind` installs the reference collector from
`examples/observability` into the kind cluster and prints what it receives.
