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

**kagent** is a Kubernetes-native framework for building, deploying, and managing AI agents at scale. It brings the power of declarative configuration and cloud-native orchestration to AI agent development, making it easy to create production-ready AI agents that integrate seamlessly with your Kubernetes infrastructure.

<div align="center">
  <img src="img/hero.png" alt="kagent Framework" width="500">
</div>

## ✨ Features

- 🚀 **Kubernetes-Native** - Deploy and manage AI agents using familiar Kubernetes resources (CRDs)
- 🤖 **Multi-LLM Support** - Works with OpenAI, Anthropic, Azure OpenAI, Google Vertex AI, Ollama, and custom models
- 🛠️ **MCP Tool Integration** - Connect to any Model Context Protocol (MCP) server for extended capabilities
- 🔌 **Pre-built Tools** - Kubernetes, Istio, Helm, Argo, Prometheus, Grafana, Cilium tooling out-of-the-box
- 📊 **Full Observability** - OpenTelemetry tracing for monitoring agent behavior and performance
- 📝 **Declarative Configuration** - Define agents and tools using simple YAML manifests
- 🎯 **Easy Testing & Debugging** - Built-in support for testing and debugging AI agent workflows
- 🌐 **Web UI & CLI** - Manage agents through intuitive web interface or command-line tools

---

## 📑 Table of Contents

- [Features](#-features)
- [Quick Start](#-quick-start)
- [Prerequisites](#-prerequisites)
- [Installation](#-installation)
- [Usage Example](#-usage-example)
- [Technical Details](#-technical-details)
- [Get Involved](#-get-involved)
- [Reference](#reference)

---

## 🚀 Quick Start

Get started with kagent in minutes:

```bash
# Install kagent using the installation guide
kubectl apply -f https://raw.githubusercontent.com/kagent-dev/kagent/main/install.yaml

# Verify installation
kubectl get agents -A
```

📖 **Full Quick Start Guide**: [kagent.dev/docs/kagent/getting-started/quickstart](https://kagent.dev/docs/kagent/getting-started/quickstart)

## 📋 Prerequisites

Before installing kagent, ensure you have:

- **Kubernetes cluster** (v1.25 or later)
- **kubectl** configured to access your cluster
- **Helm** (v3.0 or later) - for Helm-based installation
- **LLM API credentials** - for your chosen provider (OpenAI, Anthropic, etc.)

## 📦 Installation

Choose your preferred installation method:

**Using kubectl:**
```bash
kubectl apply -f https://raw.githubusercontent.com/kagent-dev/kagent/main/install.yaml
```

**Using Helm:**
```bash
helm repo add kagent https://kagent-dev.github.io/kagent
helm install kagent kagent/kagent
```

For detailed installation instructions and configuration options, see the [Installation Guide](https://kagent.dev/docs/kagent/introduction/installation).

## 💡 Usage Example

Here's a simple example of creating an AI agent with kagent:

```yaml
apiVersion: kagent.dev/v1alpha1
kind: Agent
metadata:
  name: k8s-assistant
spec:
  systemPrompt: |
    You are a helpful Kubernetes assistant. Help users manage their Kubernetes resources.
  modelConfig:
    name: gpt-4
    provider: openai
  tools:
    - name: kubernetes-tools
      type: mcp
```

Apply the agent:
```bash
kubectl apply -f agent.yaml
```

Interact with your agent via the CLI or UI:
```bash
kagent chat k8s-assistant "List all pods in the default namespace"
```

For more examples and use cases, visit our [documentation](https://kagent.dev/docs).

## 📚 Technical Details

### Core Concepts

- **Agents**: The main building block of kagent. Each agent consists of a system prompt, a set of tools and sub-agents, and an LLM configuration, all represented as a Kubernetes custom resource (`Agent` CRD). Agents can be composed and nested to create complex AI workflows.

- **LLM Providers**: Kagent supports multiple LLM providers through the `ModelConfig` resource:
  - [OpenAI](https://kagent.dev/docs/kagent/supported-providers/openai) - GPT-4, GPT-3.5, and other OpenAI models
  - [Azure OpenAI](https://kagent.dev/docs/kagent/supported-providers/azure-openai) - Enterprise-ready OpenAI models on Azure
  - [Anthropic](https://kagent.dev/docs/kagent/supported-providers/anthropic) - Claude models
  - [Google Vertex AI](https://kagent.dev/docs/kagent/supported-providers/google-vertexai) - Gemini and PaLM models
  - [Ollama](https://kagent.dev/docs/kagent/supported-providers/ollama) - Self-hosted open-source models
  - [Custom Providers](https://kagent.dev/docs/kagent/supported-providers/custom-models) - Any model accessible via AI gateways

- **MCP Tools**: Agents can connect to any [Model Context Protocol (MCP)](https://modelcontextprotocol.io) server to access tools and capabilities. Kagent includes built-in MCP servers for:
  - **Kubernetes** - Manage pods, deployments, services, and more
  - **Istio** - Service mesh operations
  - **Helm** - Package management
  - **Argo** - GitOps and workflows
  - **Prometheus & Grafana** - Observability and monitoring
  - **Cilium** - Network security and observability

  All tools are defined as Kubernetes custom resources (`ToolServer` CRD) and can be shared across multiple agents.

- **Observability**: Built-in [OpenTelemetry tracing](https://kagent.dev/docs/kagent/getting-started/tracing) provides visibility into agent execution, tool usage, and LLM interactions. Monitor agent behavior using your existing observability stack.

### Core Principles

- **Kubernetes-Native**: Leverages Kubernetes' declarative model, scaling capabilities, and ecosystem. Agents are defined as custom resources and managed like any other Kubernetes workload.

- **Extensible**: Open architecture allows you to add custom agents, tools, and LLM providers. Integrate with existing tools through MCP or build your own extensions.

- **Flexible**: Supports diverse AI agent patterns - from simple chatbots to complex multi-agent systems. Compose agents together to build sophisticated workflows.

- **Observable**: Full integration with cloud-native observability tools. OpenTelemetry tracing, Prometheus metrics, and structured logging provide complete visibility into agent behavior.

- **Declarative**: Infrastructure-as-Code approach using YAML manifests. Version control your agents, enable GitOps workflows, and manage agents programmatically.

- **Testable**: Built-in testing and debugging capabilities. Dry-run agents, inspect execution traces, and validate behavior before production deployment.

### Architecture

Kagent provides a comprehensive platform for AI agent development with four core components:

<div align="center">
  <img src="img/arch.png" alt="kagent Architecture" width="500">
</div>

**Component Overview:**

- **Controller**: A Kubernetes operator that watches kagent custom resources (Agents, ModelConfigs, ToolServers) and reconciles the desired state. Manages the lifecycle of agents and their dependencies.

- **Engine**: The agent runtime powered by [ADK (Agent Development Kit)](https://google.github.io/adk-docs/). Executes agents, handles tool invocations, manages LLM interactions, and coordinates multi-agent workflows.

- **UI**: A web-based dashboard for managing agents and monitoring their execution. Visualize agent traces, review tool usage, configure resources, and interact with agents through a chat interface.

- **CLI**: Command-line interface (`kagent`) for agent management, local development, and debugging. Interact with agents, inspect resources, and streamline your development workflow.

## 🤝 Get Involved

We welcome contributions from the community! Contributors are expected to respect the [kagent Code of Conduct](https://github.com/kagent-dev/community/blob/main/CODE-OF-CONDUCT.md).

### Ways to Contribute

- 🐛 **Report Bugs** - [File an issue](https://github.com/kagent-dev/kagent/issues/) if you find a bug
- 💡 **Request Features** - [Suggest new features](https://github.com/kagent-dev/kagent/issues/) or improvements
- 📖 **Improve Documentation** - Help make our [docs](https://github.com/kagent-dev/website/) better
- 🔧 **Submit Pull Requests** - Read our [contribution guide](/CONTRIBUTION.md) and submit PRs
- ⭐ **Star the Repository** - Show your support by starring the repo
- 💬 **Join Discord** - [Help others and get help](https://discord.gg/Fu3k65f2k3) in our community
- 📅 **Attend Community Meetings** - [Join our regular meetings](https://calendar.google.com/calendar/u/0?cid=Y183OTI0OTdhNGU1N2NiNzVhNzE0Mjg0NWFkMzVkNTVmMTkxYTAwOWVhN2ZiN2E3ZTc5NDA5Yjk5NGJhOTRhMmVhQGdyb3VwLmNhbGVuZGFyLmdvb2dsZS5jb20)
- 🤝 **Join CNCF Slack** - [Share tips and insights](https://cloud-native.slack.com/archives/C08ETST0076) in #kagent
- 🔒 **Report Security Issues** - See our [Security Policy](SECURITY.md) for reporting vulnerabilities

### 🗺️ Roadmap

Kagent is under active development. Check out our [project board](https://github.com/orgs/kagent-dev/projects/3) to see what we're working on and what's planned for future releases.

### 🛠️ Local Development

For instructions on setting up your local development environment and running kagent components locally, see our [Development Guide](DEVELOPMENT.md).

### 👥 Contributors

Thanks to all the amazing people who have contributed to making kagent better! 🎉

<a href="https://github.com/kagent-dev/kagent/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=kagent-dev/kagent" />
</a>

### ⭐ Star History

[![Star History Chart](https://api.star-history.com/svg?repos=kagent-dev/kagent&type=Date)](https://www.star-history.com/#kagent-dev/kagent&Date)

## 📖 Reference

### Documentation

- 📚 [Full Documentation](https://kagent.dev/docs)
- 🚀 [Quick Start Guide](https://kagent.dev/docs/kagent/getting-started/quickstart)
- 📦 [Installation Guide](https://kagent.dev/docs/kagent/introduction/installation)
- 🔍 [API Reference](https://kagent.dev/docs/kagent/api-reference)
- 💡 [Examples](https://github.com/kagent-dev/kagent/tree/main/examples)

### Community & Support

- 💬 [Discord Community](https://discord.gg/Fu3k65f2k3)
- 🐙 [GitHub Issues](https://github.com/kagent-dev/kagent/issues)
- 💼 [CNCF Slack #kagent](https://cloud-native.slack.com/archives/C08ETST0076)
- 🤖 [Ask DeepWiki AI](https://deepwiki.com/kagent-dev/kagent)

### License

This project is licensed under the [Apache 2.0 License](/LICENSE).

---

<div align="center">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/cncf/artwork/refs/heads/main/other/cncf/horizontal/color-whitetext/cncf-color-whitetext.svg">
      <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/cncf/artwork/refs/heads/main/other/cncf/horizontal/color/cncf-color.svg">
      <img width="300" alt="Cloud Native Computing Foundation logo" src="https://raw.githubusercontent.com/cncf/artwork/refs/heads/main/other/cncf/horizontal/color-whitetext/cncf-color-whitetext.svg">
    </picture>
    <p>kagent is a <a href="https://cncf.io">Cloud Native Computing Foundation</a> project.</p>
</div>