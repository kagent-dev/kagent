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
    args: ["static", "--host", "0.0.0.0", "--port", "8080"]
```

In this example, the HTTP listener uses port `8080`. Substrate readiness uses a
separate listener on port `8081`. A2A gRPC binds to `[::]` on `KAGENT_PORT`,
which the controller sets to `80`. The same variable selects the Go ADK's
shared HTTP/gRPC port.
