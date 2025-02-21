# kagent

This repo is a monorepo for all work related to kagent.

## autogen extensions

The autogen extensions are located in the `python` directory. For more information, see the [README](python/README.md).

## controller

The controller is located in the `controller` directory. For more information, see the [README](controller/README.md).


## Examples

The examples are located in the `examples` directory. For more information, see the [README](examples/README.md).


## How to run everything locally

Running outside Kubernetes:


1. Run the backend from the `python` folder:

```bash
uv sync --all-extras

# Run the autogen backend
uv run autogenstudio ui
```

If you get an error that looks like this:

```
Smudge error: Error downloading...
```

Set the `GIT_LFS_SKIP_SMUDGE=1` variable and then run sync command.

2. Run the frontend from the `ui` folder:

```bashG
npm install

npm run dev
```