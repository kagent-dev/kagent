# Development

To understand how to develop for kagent, It's important to understand the architecture of the project. Please refer to the [README.md](README.md#architecture) file for an overview of the project.

When making changes to `kagent`, the most important thing is to figure out which piece of the project is affected by the change, and then make the change in the appropriate folder. Each piece of the project has its own README with more information about how to setup the development environment and run that piece of the project.

- [python](python): Contains the code for the ADK  engine.
- [go](go): Contains the code for the kubernetes controller, and the CLI.
- [ui](ui): Contains the code for the web UI.


## How to run everything in Kubernetes

1. Create a cluster:

```shell
make create-kind-cluster
```

2. Set your providers API_KEY:

```shell
export OPENAI_API_KEY=your-openai-api-key
#or
export ANTHROPIC_API_KEY=your-anthropic-api-key
```

3. Build images, load them into kind cluster and deploy everything using Helm:

```shell
make helm-install
```

To access the UI, port-forward to the UI port on the `kagent-ui` service:

```shell
kubectl port-forward svc/kagent-ui 8001:80
```

Then open your browser and go to `http://localhost:8001`.

