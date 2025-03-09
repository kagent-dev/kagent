# kagent

kagent is a kubernetes native framework for building AI agents. Kubernets is the most popular orchestration platform for running workloads, and kagent makes it easy to build, deploy and manage AI agents in kubernetes. The kagent framework is designed to be easy to understand and use, and to provide a flexible and powerful way to build and manage AI agents.

The core concepts of the kagent framework are:

- **Agents**: Agents are the main building block of kagent. They are a system prompt, a set of tools, and a model configuration.
- **Tools**: Tools are any external tool that can be used by an agent. They are defined as kubernetes custom resources and can be used by multiple agents.

All of the above are defined as kubernetes custom resources, which makes them easy to manage and modify.



## Quick start

1. Install helm, and kubectl.
2. Install the helm chart: `helm install kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent`
3. Port-forward the UI: `kubectl port-forward svc/kagent-ui 8080:80`


## Local development

For instructions on how to run everything locally, see the [DEVELOPMENT.md](DEVELOPMENT.md) file.

## Contributing

For instructions on how to contribute to the kagent project, see the [CONTRIBUTION.md](CONTRIBUTION.md) file.
