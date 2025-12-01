# K8sGPT MCP Server

This directory contains the Kubernetes deployment and configuration files for running [K8sGPT](https://github.com/k8sgpt-ai/k8sgpt) as an MCP (Model Context Protocol) server within the KAgent ecosystem.

## What is K8sGPT?

K8sGPT is an AI-powered Kubernetes troubleshooting tool that analyzes your cluster and provides intelligent insights about issues, errors, and potential problems. It can:

- Analyze Kubernetes resources and identify issues
- Provide AI-powered explanations and solutions
- Offer anonymization for sensitive data
- Integrate with custom analyzers

## Installation

### 1. Deploy K8sGPT MCP Server

Deploy the K8sGPT MCP server to your Kubernetes cluster:

```bash
kubectl apply -f deploy-k8sgpt-mcp-server.yaml
```

This will create:
- ServiceAccount with cluster-wide permissions
- ClusterRole and ClusterRoleBinding for resource access
- Service exposing ports 8080 (HTTP), 8081 (metrics), and 8089 (MCP)
- Deployment running K8sGPT with MCP server enabled

### 2. Configure the ToolServer

After deployment, configure the toolserver in KAgent:

```bash
kubectl apply -f k8sgpt-mcp-toolserver.yaml
```

This creates a ToolServer resource that connects KAgent to the K8sGPT MCP server.

## Configuration

### Environment Variables

The deployment includes these key environment variables:

- `K8SGPT_MODEL`: AI model to use (default: gpt-3.5-turbo)
- `K8SGPT_BACKEND`: AI backend provider (default: openai)
- `XDG_CONFIG_HOME`: Configuration directory path
- `XDG_CACHE_HOME`: Cache directory path

### Ports

- **8080**: HTTP API endpoint
- **8081**: Metrics endpoint
- **8089**: MCP protocol endpoint

### Resource Requirements

- **CPU**: 0.2 request, 1.0 limit
- **Memory**: 156Mi request, 512Mi limit

## Usage

Once deployed and configured, K8sGPT will be available as an MCP server within KAgent, providing AI-powered Kubernetes analysis capabilities.


## Troubleshooting

Check the deployment status:

```bash
kubectl get pods -n kagent -l app.kubernetes.io/name=k8sgpt
kubectl logs -n kagent -l app.kubernetes.io/name=k8sgpt
```

## Learn More

- [K8sGPT GitHub Repository](https://github.com/k8sgpt-ai/k8sgpt)
- [K8sGPT Documentation](https://docs.k8sgpt.ai/)
- [MCP Protocol](https://modelcontextprotocol.io/)

