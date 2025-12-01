# kagent

## Prerequisites
- [uv package manager](https://docs.astral.sh/uv/getting-started/installation/)
- Open AI API key

## Python

Firstly setup a virtual environment:
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

The package directory contains various sub-packages which comprise the kagent engine. Each framework which kagent supports has its own package. Currently that is only ADK.

In addition there is a top-level kagent package which contains the main entry point for the engine. In the future we may want to have separate entrypoints for each framework to reduce the number of dependencies we have to install.