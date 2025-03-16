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
uv python install 3.12
```

Once we have python installed, we can download the dependencies:

```bash
uv sync --all-extras
```

## Running the engine

```bash
uv run kagent-engine serve
```

## Testing

We use pytest to run tests.

```bash
uv run pytest tests/
```

