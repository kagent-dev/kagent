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
    <a href="https://deepwiki.com/kagent-dev/kagent"><img src="https://deepwiki.com/badge.svg" alt="Ask DeepWiki"></a>
    <a href='https://codespaces.new/kagent-dev/kagent'>
      <img src='https://github.com/codespaces/badge.svg' alt='Open in Github Codespaces' style='max-width: 100%;' height="20">
    </a>
    <a href="https://www.bestpractices.dev/projects/10723"><img src="https://www.bestpractices.dev/projects/10723/badge" alt="OpenSSF Best Practices"></a>
  </div>
</div>

---

**kagent** is a Kubernetes native framework for building AI agents. Kubernetes is the most popular orchestration platform for running workloads, and **kagent** makes it easy to build, deploy and manage AI agents in Kubernetes. The **kagent** framework is designed to be easy to understand and use, and to provide a flexible and powerful way to build and manage AI agents.

<div align="center">
  <img src="img/kagent-agents-ui.gif" alt="Kagent Agents UI" width="800">
</div>

---

<!-- markdownlint-disable MD033 -->
<table align="center">
  <tr>
    <td>
      <a href="#getting-started"><b><i>Getting Started</i></b></a>
    </td>
    <td>
      <a href="#technical-details"><b><i>Technical Details</i></b></a>
    </td>
    <td>
      <a href="#get-involved"><b><i>Get Involved</i></b></a>
    </td>
    <td>
      <a href="#reference"><b><i>Reference</i></b></a>
    </td>
  </tr>
</table>
<!-- markdownlint-enable MD033 -->

---

## Getting Started

- [Your first agent](https://kagent.dev/docs/kagent/1.x/get-started/your-first-agent/)
- [Installation guide](https://kagent.dev/docs/kagent/1.x/setup/installation/)

## Technical Details

### Core Concepts

- **Agents**: Agents are the main building block of kagent. An Agent is a Kubernetes custom resource that brings together an agent's behavior and runtime configuration.
- **Agent Templates**: A system prompt, a set of tools and subagents, and an LLM configuration define an agent's behavior. You can define this directly in an Agent or share it across agents using an AgentTemplate resource.
- **Harnesses**: A Harness defines how an agent runs. Kagent supports its own Go and Python ADKs, Codex, Claude, and custom runtimes, so you can choose the runtime that fits your agent.
- **LLM Providers**: Kagent supports multiple LLM providers, including [OpenAI](https://kagent.dev/docs/kagent/1.x/setup/model-providers/openai/), [Azure OpenAI](https://kagent.dev/docs/kagent/1.x/setup/model-providers/azure-openai/), [Anthropic](https://kagent.dev/docs/kagent/1.x/setup/model-providers/anthropic/), [Google Vertex AI](https://kagent.dev/docs/kagent/1.x/setup/model-providers/google-vertexai/), [Ollama](https://kagent.dev/docs/kagent/1.x/setup/model-providers/ollama/) and custom providers and models accessible via AI gateways. Models are configured through the ModelConfig resource, with provider support depending on the chosen Harness.
- **MCP Tools**: Agents can connect to MCP servers that provide tools. Kagent comes with an MCP server with tools for Kubernetes, Istio, Helm, Argo, Prometheus, Grafana, Cilium, and others. Agents connect through RemoteMCPServer resources, which can be shared across agents.
- **Sessions**: Sessions keep conversations and task history across interactions. You can suspend and resume an agent, or create a checkpoint and fork a new conversation from it.
- **Observability**: Kagent supports [OpenTelemetry tracing](https://kagent.dev/docs/kagent/1.x/observability/tracing/), which allows you to monitor what's happening with your agents and tools.

### Core Principles

- **Kubernetes Native**: Manage agent and tool configuration through Kubernetes APIs and familiar `kubectl` workflows.
- **Extensible**: Add custom tools, skills, and runtimes to connect agents to your own systems.
- **Flexible**: Choose the models and runtimes that fit your workload, and reuse agent templates across compatible harnesses.
- **Observable**: Trace agent, model, and tool execution with OpenTelemetry and your existing observability stack.
- **Declarative**: Define agents in YAML, keep configuration in version control, and deploy through GitOps workflows.
- **Testable**: Test agents through their public APIs and use task history and traces to diagnose failures.

### Architecture

```mermaid
flowchart TD
    UI[UI] --> CONTROLLER[kagent Controller]
    CLI[CLI] --> CONTROLLER
    CONFIG[Kubernetes resources] --> CONTROLLER
    CONTROLLER --> DB[(PostgreSQL)]
    CONTROLLER --> SUBSTRATE[Substrate]
    SUBSTRATE --> RUNTIMES[Agent runtimes]
    CONTROLLER -->|A2A| RUNTIMES
```

Kagent brings together the following components:

- **Controller**: The controller watches kagent custom resources, prepares agents to run, and manages their sessions. It exposes gRPC, A2A, and MCP APIs for clients to interact with agents.
- **UI**: The UI is a web UI that allows you to manage agents and tools and chat with your agents.
- **Agent Runtimes**: Agents run using the Go or Python ADK, Codex, Claude, or a custom runtime, selected by their Harness.
- **Substrate**: Substrate runs the agents and manages their lifecycle, including suspension, resumption, and snapshots.
- **PostgreSQL**: PostgreSQL stores sessions, tasks, and conversation history so they persist independently of the running agents.
- **CLI**: The CLI is a command-line tool that allows you to manage agents and tools and interact with your agents.

For more details on the v1 architecture, including resource definitions and how agents run, see the [architecture guide](docs/architecture/README.md).

## Get Involved

_We welcome contributions! Contributors are expected to [respect the kagent Code of Conduct](https://github.com/kagent-dev/community/blob/main/CODE-OF-CONDUCT.md)_

There are many ways to get involved:

- 🐛 [Report bugs and issues](https://github.com/kagent-dev/kagent/issues/)
- 💡 [Suggest new features](https://github.com/kagent-dev/kagent/issues/)
- 📖 [Improve documentation](https://github.com/kagent-dev/website/)
- 🔧 [Submit pull requests](/CONTRIBUTING.md)
- ⭐ Star the repository
- 💬 [Help others in Discord](https://discord.gg/Fu3k65f2k3)
- 💬 [Join the kagent community meetings](https://calendar.google.com/calendar/u/0?cid=Y183OTI0OTdhNGU1N2NiNzVhNzE0Mjg0NWFkMzVkNTVmMTkxYTAwOWVhN2ZiN2E3ZTc5NDA5Yjk5NGJhOTRhMmVhQGdyb3VwLmNhbGVuZGFyLmdvb2dsZS5jb20)
- 🤝 [Share tips in the CNCF #kagent slack channel](https://cloud-native.slack.com/archives/C08ETST0076)
- 🔒 [Report security concerns](SECURITY.md)

### Roadmap

`kagent` is currently in active development. You can check out the full roadmap in the project Kanban board [here](https://github.com/orgs/kagent-dev/projects/3).

### Local development

For instructions on how to run everything locally, see the [DEVELOPMENT.md](DEVELOPMENT.md) file.

### Contributors

Thanks to all contributors who are helping to make kagent better.

<a href="https://github.com/kagent-dev/kagent/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=kagent-dev/kagent" />
</a>

### Star History

<a href="https://www.star-history.com/#kagent-dev/kagent&Date">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=kagent-dev/kagent&type=Date&theme=dark" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=kagent-dev/kagent&type=Date" />
   <img alt="Star history of kagent-dev/kagent over time" src="https://api.star-history.com/svg?repos=kagent-dev/kagent&type=Date" />
 </picture>
</a>

## Reference

### License

This project is licensed under the [Apache 2.0 License.](/LICENSE)

---

<div align="center">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/cncf/artwork/refs/heads/main/other/cncf/horizontal/color-whitetext/cncf-color-whitetext.svg">
      <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/cncf/artwork/refs/heads/main/other/cncf/horizontal/color/cncf-color.svg">
      <img width="300" alt="Cloud Native Computing Foundation logo" src="https://raw.githubusercontent.com/cncf/artwork/refs/heads/main/other/cncf/horizontal/color-whitetext/cncf-color-whitetext.svg">
    </picture>
    <p>kagent is a <a href="https://cncf.io">Cloud Native Computing Foundation</a> project.</p>
</div>
