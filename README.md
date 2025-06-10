<div align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/kagent-dev/kagent/main/img/icon-dark.svg" alt="kagent" width="400">
    <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/kagent-dev/kagent/main/img/icon-light.svg" alt="kagent" width="400">
    <img alt="kagent" src="https://raw.githubusercontent.com/kagent-dev/kagent/main/img/icon-light.svg">
  </picture>
  <div>
     <a href="https://discord.gg/Fu3k65f2k3">
      <img src="https://img.shields.io/discord/1346225185166065826?style=flat&label=Join%20Discord&color=6D28D9" alt="Discord">
    </a>
  </div>
</div>

---

**kagent** is a Kubernetes native framework for building AI agents. Kubernetes is the most popular orchestration platform for running workloads, and **kagent** makes it easy to build, deploy and manage AI agents in kubernetes. The **kagent** framework is designed to be easy to understand and use, and to provide a flexible and powerful way to build and manage AI agents.

<div align="center">
  <img src="img/hero.png" alt="Autogen Framework" width="500">
</div>

  <!--latest build-->
  <a href="https://github.com/kagent-dev/kagent/actions/workflows/ci.yaml">
    <img src="https://github.com/kagent-dev/kagent/actions/workflows/ci.yaml/badge.svg" alt="Build Status" height="25">
  </a>

---

## Get started

<div align="center">
  <!--codespaces-->
  <a href='https://codespaces.new/kagent-dev/kagent'>
    <img src='https://github.com/codespaces/badge.svg' alt='Open in Github Codespaces' style='max-width: 100%;' height="26">
  </a>
</div>

- [Quick Start](https://kagent.dev/docs/getting-started/quickstart)
- [Installation guide](https://kagent.dev/docs/introduction/installation)


## Documentation

The kagent documentation is available at [kagent.dev/docs](https://kagent.dev/docs).

## Core Concepts

- **Agents**: Agents are the main building block of kagent. They are a system prompt, a set of tools, and a model configuration.
- **Tools**: Tools are any external tool that can be used by an agent. They are defined as Kubernetes custom resources and can be used by multiple agents.

All of the above are defined as Kubernetes custom resources, which makes them easy to manage and modify.

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

`kagent` is currently in active development. The following is a list of features that are planned for the next few releases.

- [ ] [Observability improvements:](https://github.com/kagent-dev/kagent/issues/130)
  - [ ] More powerful Tracing capabilities
  - [ ] Tighter oTEL integration
  - [ ] Metrics
- [ ] [Feedback/Testing:](https://github.com/kagent-dev/kagent/issues/131)
  - [ ] Eval framework/integrations
  - [ ] Debugging/Time travel
  - [ ] Guided Learning
- [ ] [Runtime/Engine improvements:](https://github.com/kagent-dev/kagent/issues/132)
  - [ ] Multi-Agent support
  - [ ] True Graph Execution
  - [ ] Workflows
  - [x] Multiple LLM Provider support
- [ ] [Tools:](https://github.com/kagent-dev/kagent/issues/133)
  - [x] Tool Discovery
  - [x] Expose built-in tools as MCP server

## Local development

For instructions on how to run everything locally, see the [DEVELOPMENT.md](DEVELOPMENT.md) file.

## Contributing

For instructions on how to contribute to the kagent project, see the [CONTRIBUTION.md](CONTRIBUTION.md) file.
