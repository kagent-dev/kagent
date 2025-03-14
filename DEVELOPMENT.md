# Development

To understand how to develop for kagent, It's important to understand the architecture of the project. Please refer to the [README.md](README.md#architecture) file for an overview of the project.

When making changes to `kagent`, the most important thing is to figure out which piece of the project is affected by the change, and then make the change in the appropriate folder. Each piece of the project has its own README with more information about how to setup the development environment and run that piece of the project.

- [python](python): Contains the code for the autogen engine.
- [go](go): Contains the code for the kubernetes controller, and the CLI.
- [ui](ui): Contains the code for the web UI.


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

```bash
npm install

npm run dev
```

## How to run everything in Kubernetes

1. Create a cluster:

```shell
make create-kind-cluster
```

2. Set your OPENAI_API_KEY:

```shell
export OPENAI_API_KEY=your-openai-api-key
```

3. Build images, load them into kind cluster and deploy everything using Helm:

```shell
make helm-install
```

To access the UI, port-forward to the app service:

```shell
kubectl port-forward svc/app 8001:80
```

Then open your browser and go to `http://localhost:8001`.