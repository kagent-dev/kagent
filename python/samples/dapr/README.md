# Dapr-Agents DurableAgent Sample

Demonstrates KAgent integration with Dapr-Agents DurableAgent for durable workflow-based agent execution.

## Features

- Dapr-Agents DurableAgent with durable workflow orchestration
- A2A protocol compatibility via kagent-dapr
- Kubernetes deployment with Dapr sidecar injection

## Prerequisites

- Kubernetes cluster with kagent installed
- Dapr installed on the cluster
- Redis
- kubectl configured for the cluster

## Quick Start

1. Install Dapr on your cluster:

```bash
helm repo add dapr https://dapr.github.io/helm-charts/
helm install dapr dapr/dapr --namespace dapr-system --create-namespace
```

2. Apply Dapr components (includes Redis deployment and all state/pubsub components):

```bash
kubectl apply -f components/
```

3. Build and push the agent image:

Run the `dapr-agent-sample` target from the top-level Python directory.

```bash
make dapr-agent-sample
```

4. Create the LLM API key secret:

```bash
kubectl create secret generic kagent-llm-key -n default \
  --from-literal=API_KEY=$API_KEY \
  --dry-run=client -o yaml | kubectl apply -f -
```

5. Deploy the agent:

```bash
kubectl apply -f agent.yaml
```

## Local Development

1. Install dependencies:

```bash
uv sync
```

2. Set environment variables:

```bash
export API_KEY=your_api_key_here
export KAGENT_URL=http://localhost:8083
```

3. Run with Dapr sidecar:

```bash
dapr run --app-id my-dapr-agent --app-port 8080 -- uv run python agent.py
```

## Dapr Components

| Component | Type | Purpose |
|-----------|------|---------|
| `redis` | Deployment + Service + Secret | Redis instance for all Dapr state/pubsub |
| `agent-registry` | `state.redis` | Agent state/registry storage |
| `agent-memory` | `state.redis` | Agent conversation memory |
| `agent-pubsub` | `pubsub.redis` | Agent pub/sub messaging |
| `agent-runtimestatestore` | `state.redis` | Dapr runtime state |
| `agent-workflow` | `state.redis` | Durable workflow state (actor state store) |

## Configuration

- `API_KEY`: LLM API key
- `PORT`: Server port (default: `8080`)
- `KAGENT_URL`: KAgent controller URL (default: `http://kagent-controller.kagent:8083`)

## Architecture

`KAgentApp` builds a FastAPI application that integrates with the A2A protocol. When a request arrives, `DaprDurableAgentExecutor` schedules a Dapr durable workflow via the `DurableAgent`, waits for completion, and emits A2A status and artifact events back to the caller.
