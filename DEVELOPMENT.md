# Development

To understand how to develop for kagent, It's important to understand the architecture of the project. Please refer to the [README.md](README.md#architecture) file for an overview of the project.

When making changes to `kagent`, the most important thing is to figure out which piece of the project is affected by the change, and then make the change in the appropriate folder. Each piece of the project has its own README with more information about how to setup the development environment and run that piece of the project.

- [python](python): Contains the code for the ADK  engine.
- [go](go): Contains the code for the kubernetes controller, and the CLI.
- [ui](ui): Contains the code for the web UI.

## Dependencies

Before you can run kagent in Kubernetes, you need to have the following tools installed:

### Required Dependencies

- **Kind** (v0.27.0+)
- **kubectl** (v1.33.4+)
- **Helm**
- **Go** (v1.24.6+)
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

1. Create a cluster:

```shell
make create-kind-cluster
```

2. Set your model provider:

```shell
export KAGENT_DEFAULT_MODEL_PROVIDER=openAI
#or
export KAGENT_DEFAULT_MODEL_PROVIDER=anthropic
```

3. Set your providers API_KEY:

```shell
export OPENAI_API_KEY=your-openai-api-key
#or
export ANTHROPIC_API_KEY=your-anthropic-api-key
```

4. Build images, load them into kind cluster and deploy everything using Helm:

```shell
make helm-install
```

To access the UI, port-forward to the UI port on the `kagent-ui` service:

```shell
kubectl port-forward svc/kagent-ui 8001:80
```

Then open your browser and go to `http://localhost:8001`.

### Addons

Optional addons are available to enhance your development environment with
observability and infrastructure components.

**Prerequisites:** Complete steps 1-3 above (cluster creation and environment variables).

To install all addons:

```shell
make kagent-addon-install
```

This installs the following components into your cluster:

| Addon          | Description                                         | Namespace |
|----------------|-----------------------------------------------------|-----------|
| Istio          | Service mesh (demo profile)                         | `istio-system` |
| Grafana        | Dashboards and visualization                        | `kagent` |
| Prometheus     | Metrics collection                                  | `kagent` |
| Metrics Server | Kubernetes resource metrics                         | `kube-system` |
| Postgres       | Relational database (for kagent controller storage) | `kagent` |

#### Using Postgres as the Datastore

By default, kagent uses a local SQLite database for data persistence. To use
postgres as the backing store instead, deploy kagent via:

> **Warning:**  
> The following example uses hardcoded Postgres credentials (`postgres:kagent`) for local development only.  
> **Do not use these credentials in production environments.**  
```shell
KAGENT_HELM_EXTRA_ARGS="--set database.type=postgres --set database.postgres.url=postgres://postgres:kagent@postgres.kagent.svc.cluster.local:5432/kagent" \
  make helm-install
```

Verify the connection by checking the controller logs:

```shell
kubectl logs -n kagent deployment/kagent-controller | grep -i postgres
```

**To revert to SQLite:** Run `make helm-install` without the `KAGENT_HELM_EXTRA_ARGS` variable.

### Troubleshooting

### buildx localhost access

The `make helm-install` command might time out with an error similar to the following:

> ERROR: failed to solve: DeadlineExceeded: failed to push localhost:5001/kagent-dev/kagent/controller

As part of the build process, the `buildx` container tries to build and push the kagent images to the local Docker registry. The `buildx` command requires access to your host machine's Docker daemon.

Recreate the buildx builder with host networking, such as with the following example commands. Update the version and platform accordingly.

```shell
docker buildx rm kagent-builder-v0.23.0

docker buildx create --name kagent-builder-v0.23.0 --platform linux/amd64,linux/arm64 --driver docker-container --use --driver-opt network=host
```

Then run the `make helm-install` command again.

### Run kagent and an agent locally.

create a minimal cluster with kind. scale kagent to 0 replicas, as we will run it locally.

```bash
make create-kind-cluster helm-install-provider helm-tools push-test-agent push-test-skill
kubectl scale -n kagent deployment kagent-controller --replicas 0
```

Run kagent with `KAGENT_A2A_DEBUG_ADDR=localhost:8080` environment variable set, and when it connect to agents it will go to "localhost:8080" instead of the Kubernetes service.

Run the agent locally as well, with `--net=host` option, so it can connect to the kagent service on localhost. For example:

```bash
docker run --rm \
  -e KAGENT_URL=http://localhost:8083 \
  -e KAGENT_NAME=kebab-agent \
  -e KAGENT_NAMESPACE=kagent \
  --net=host \
  localhost:5001/kebab:latest
```
