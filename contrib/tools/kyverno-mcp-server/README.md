# Kyverno MCP Server

This directory contains the Kubernetes deployment and configuration files for running the [Kyverno MCP Server](https://github.com/Fulcria-Labs/kyverno-mcp-server) within the kagent ecosystem.

## What is Kyverno?

[Kyverno](https://kyverno.io/) is a CNCF Graduated policy engine for Kubernetes. It allows cluster administrators to manage security, compliance, and best practices using policies as Kubernetes resources. The Kyverno MCP Server makes these policy operations accessible to AI agents.

## Capabilities

The MCP server exposes 8 tools for policy management:

| Tool | Description |
|------|-------------|
| `list_policies` | List ClusterPolicies or namespace-scoped policies |
| `get_policy` | Get detailed policy configuration |
| `explain_policy` | Human-readable explanation of what a policy does |
| `list_policy_reports` | Compliance status from policy reports |
| `get_policy_violations` | Find non-compliant resources |
| `check_resource_compliance` | Check if a specific resource is compliant |
| `generate_policy` | Generate common policy templates |
| `get_compliance_summary` | Cluster-wide compliance percentage |

## Installation

### Prerequisites

- Kubernetes cluster with [Kyverno](https://kyverno.io/docs/installation/) installed
- kagent deployed to the cluster

### 1. Build and Load the MCP Server Image

```bash
# Clone the MCP server repo
git clone https://github.com/Fulcria-Labs/kyverno-mcp-server.git
cd kyverno-mcp-server

# Build the container image
docker build -t kyverno-mcp-server:latest .

# If using Kind, load the image
kind load docker-image kyverno-mcp-server:latest --name kagent
```

### 2. Deploy the MCP Server

```bash
kubectl apply -f deploy-kyverno-mcp-server.yaml
```

This creates:
- ServiceAccount with read-only access to Kyverno CRDs and policy reports
- ClusterRole and ClusterRoleBinding
- Service exposing port 8089 (MCP)
- Deployment running the MCP server

### 3. Register with kagent

```bash
kubectl apply -f kyverno-remote-mcpserver.yaml
```

### 4. Create the Kyverno Agent

```bash
kubectl apply -f kyverno-agent.yaml
```

## Usage

Once deployed, the Kyverno agent will appear in the kagent UI. You can ask it questions like:

- "What policies are deployed in my cluster?"
- "Are there any policy violations?"
- "Explain the disallow-privileged policy"
- "Generate a policy to require resource limits"
- "What's the overall compliance status?"

## Troubleshooting

```bash
# Check MCP server status
kubectl get pods -n kagent -l app.kubernetes.io/name=kyverno-mcp-server
kubectl logs -n kagent -l app.kubernetes.io/name=kyverno-mcp-server

# Verify Kyverno is installed
kubectl get crd | grep kyverno
```

## Learn More

- [Kyverno Documentation](https://kyverno.io/docs/)
- [Kyverno MCP Server Source](https://github.com/Fulcria-Labs/kyverno-mcp-server)
- [MCP Protocol](https://modelcontextprotocol.io/)
