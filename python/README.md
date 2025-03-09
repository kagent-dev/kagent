# kagent

## Prerequisites
- [uv package manager](https://docs.astral.sh/uv/getting-started/installation/)
- Open AI API key

## Python

We use uv to manage dependencies as well as the python version.

```bash
uv python install 3.12
```

## Running python code

First we build and install dependencies:
```bash
uv sync --all-extras
```

## Testing

We use pytest to run tests.

```bash
uv run pytest tests/
```

