# Kebab Agent

This agent can be used to test KAgent's Remote Agent functionality.

## Steps

### 1. Build the agent image

```bash
docker build . --push -t localhost:5001/remote-kebab:latest
```

### 2. Deploy the agent

```bash
kubectl apply -f agent.yaml
```
