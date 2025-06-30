<div align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/kagent-dev/kagent/main/img/icon-dark.svg" alt="kagent" width="400">
    <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/kagent-dev/kagent/main/img/icon-light.svg" alt="kagent" width="400">
    <img alt="kagent" src="https://raw.githubusercontent.com/kagent-dev/kagent/main/img/icon-light.svg">
  </picture>
  <div>
    <a href="https://github.com/kagent-dev/kagent/releases">
      <img src="https://img.shields.io/github/v/release/kagent-dev/kagent?style=flat&label=Latest%20version" alt="Release">
    </a>
    <a href="https://github.com/kagent-dev/kagent/actions/workflows/ci.yaml">
      <img src="https://github.com/kagent-dev/kagent/actions/workflows/ci.yaml/badge.svg" alt="Build Status" height="20">
    </a>
      <a href="https://opensource.org/licenses/Apache-2.0">
      <img src="https://img.shields.io/badge/License-Apache2.0-brightgreen.svg?style=flat" alt="License: Apache 2.0">
    </a>
    <a href="https://github.com/kagent-dev/kagent">
      <img src="https://img.shields.io/github/stars/kagent-dev/kagent.svg?style=flat&logo=github&label=Stars" alt="Stars">
    </a>
     <a href="https://discord.gg/Fu3k65f2k3">
      <img src="https://img.shields.io/discord/1346225185166065826?style=flat&label=Join%20Discord&color=6D28D9" alt="Discord">
    </a>
    <a href='https://codespaces.new/kagent-dev/kagent'>
      <img src='https://github.com/codespaces/badge.svg' alt='Open in Github Codespaces' style='max-width: 100%;' height="20">
    </a>
  </div>
</div>

---

**kagent** is a Kubernetes native framework for building AI agents. Kubernetes is the most popular orchestration platform for running workloads, and **kagent** makes it easy to build, deploy and manage AI agents in Kubernetes. The **kagent** framework is designed to be easy to understand and use, and to provide a flexible and powerful way to build and manage AI agents.

<div align="center">
  <img src="img/hero.png" alt="Autogen Framework" width="500">
</div>

---

## Get started

- [Quick Start](https://kagent.dev/docs/getting-started/quickstart)
- [Installation guide](https://kagent.dev/docs/introduction/installation)


## Documentation

The kagent documentation is available at [kagent.dev/docs](https://kagent.dev/docs).

## Core Concepts

- **Agents**: Agents are the main building block of kagent. They are a system prompt, a set of tools and agents, and an LLM configuration represented with a Kubernetes custom resource called "Agent". 
- **LLM Providers**: Kagent supports multiple LLM providers, including [OpenAI](https://kagent.dev/docs/supported-providers/openai), [Azure OpenAI](https://kagent.dev/docs/supported-providers/azure-openai), [Anthropic](https://kagent.dev/docs/supported-providers/anthropic), [Google Vertex AI](https://kagent.dev/docs/supported-providers/google-vertexai), [Ollama](https://kagent.dev/docs/supported-providers/ollama) and any other [custom providers and models](https://kagent.dev/docs/supported-providers/custom-models) accessible via AI gateways. Providers are represented by the ModelConfig resource.
- **MCP Tools**: Agents can connect to any MCP server that provides tools. Kagent comes with an MCP server with tools for Kubernetes, Istio, Helm, Argo, Prometheus, Grafana,  Cilium and others. All tools are as Kubernetes custom resources (ToolServers) and can be used by multiple agents.
- **Memory**: Using the [memory](https://kagent.dev/docs/concepts/memory) your agents can always have access to the latest and most up to date information.
- **Observability**: Kagent supports [OpenTelemetry tracing](https://kagent.dev/docs/getting-started/tracing) which allows you to monitor what's happening with your agents and tools.

## Core Principles

- **Kubernetes Native**: Kagent is designed to be easy to understand and use, and to provide a flexible and powerful way to build and manage AI agents.
- **Extensible**: Kagent is designed to be extensible, so you can add your own agents and tools.
- **Flexible**: Kagent is designed to be flexible, to suit any AI agent use case.
- **Observable**: Kagent is designed to be observable, so you can monitor the agents and tools using all common monitoring frameworks.
- **Declarative**: Kagent is designed to be declarative, so you can define the agents and tools in a yaml file.
- **Testable**: Kagent is designed to be tested and debugged easily. This is especially important for AI agent applications.

## Architecture

The kagent framework is designed to be easy to understand and use, and to provide a flexible and powerful way to build and manage AI agents.

<div align="center">
  <img src="img/arch.png" alt="Autogen Framework" width="500">
</div>

Kagent has 4 core components:

- **Controller**: The controller is a Kubernetes controller that watches the kagent custom resources and creates the necessary resources to run the agents.
- **UI**: The UI is a web UI that allows you to manage the agents and tools.
- **Engine**: The engine is a Python application that runs the agents and tools. The engine is built using [Autogen](https://github.com/microsoft/autogen).
- **CLI**: The CLI is a command line tool that allows you to manage the agents and tools.

## Roadmap

`kagent` is currently in active development. You can check out the full roadmap in the project Kanban board [here](https://github.com/orgs/kagent-dev/projects/3).

## Local development

For instructions on how to run everything locally, see the [DEVELOPMENT.md](DEVELOPMENT.md) file.

## Contributing

For instructions on how to contribute to the kagent project, see the [CONTRIBUTION.md](CONTRIBUTION.md) file.

## Contributors

Thanks to all contributors who are helping to make kagent better.

<a href="https://github.com/kagent-dev/kagent/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=kagent-dev/kagent" />
</a>

## Star History

<a href="https://www.star-history.com/#kagent-dev/kagent&Date">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=kagent-dev/kagent&type=Date&theme=dark" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=kagent-dev/kagent&type=Date" />
   <img alt="Star history of kagent-dev/kagent over time" src="https://api.star-history.com/svg?repos=kagent-dev/kagent&type=Date" />
 </picture>
</a>
