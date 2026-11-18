# kagent

## Prerequisites

- [uv package manager](https://docs.astral.sh/uv/getting-started/installation/)
- OpenAI API key

## Python

First, set up a virtual environment:

```bash
uv venv .venv
```

We use uv to manage dependencies as well as the python version.

```bash
uv python install
```

Once we have python installed, we can download the dependencies:

```bash
uv sync --all-extras
```

## Running the engine

The python code in this project uses the UV workspaces to manage the dependencies. You can read about them [here](https://docs.astral.sh/uv/concepts/projects/workspaces/).

The package directory contains various sub-packages which comprise the kagent engine. Each framework which kagent supports has its own package.

In addition there is a top-level kagent package which contains the main entry point for the engine. In the future we may want to have separate entrypoints for each framework to reduce the number of dependencies we have to install.

### Runtime entrypoint

The Python ADK image uses Debian Bookworm with a standalone Python interpreter
installed by uv. uv installs production dependencies from `uv.lock` during the
build. The final image includes uv, the interpreter, and the application
environment, without development dependencies, pip, or its `ensurepip` bootstrap
bundle.

For additional packages, create a separate environment in the writable `/config`
directory using the image's Python interpreter:

```bash
uv venv /config/tools
uv pip install --python /config/tools/bin/python <package>
```

uv caches downloads in `/config/.cache/uv`. Interpreter downloads are disabled;
the image supplies Python. The application environment at `/.kagent/.venv` stays
owned by root, while the image runs as user `65532`.

The Python ADK image defaults to `kagent-adk run`, which expects a named
agent directory. To load controller-generated configuration with a `kagent`
Harness instead, select `kagent-adk static` as shown in this Harness excerpt:

```yaml
spec:
  kagent: {}
  workload:
    command: ["/.kagent/.venv/bin/kagent-adk"]
    args: ["static"]
```

Substrate probes `GET /readyz` on port `8081` after the gRPC listener starts. A2A gRPC binds to `[::]` on `runtime.port`,
which the controller sets to `80`. The same variable selects the Go ADK's
shared HTTP/gRPC port.

### Invoking a BYO agent

Configure a BYO Harness with the image and reference the Harness from an Agent.
Substrate starts the workload with the controller’s revision configuration and
agent card. Discover the Agent at
`GET /agents/{namespace}/{name}/.well-known/agent-card.json` on the controller
gateway. Send messages to the advertised interface and use the returned context
ID for subsequent turns. See [public A2A transports](../docs/architecture/a2a-transports.md).

### Updating existing Python agents

Replace `uvicorn.run(app.build(), ...)` with `app.build().run()` and launch the
Python module directly. Replace OpenAI’s `build_local()` with `build()` and use a managed Agent for integration tests. An agent package can still export a custom ADK `lifespan(server)` async context manager.
Its setup runs before the runtime binds its listeners. Its cleanup runs after the listeners and controller client close, including when listener startup fails.
Move HTTP health checks to `GET /readyz` on port `8081`. Send JSON-RPC requests
and fetch agent cards through the [controller gateway](../docs/architecture/a2a-transports.md).

Remove the Uvicorn flags `--host`, `--port`, `--workers`, and `--reload` from
Python ADK workload commands. The gRPC listener reads `runtime.host` and
`runtime.port` from the runtime configuration.
