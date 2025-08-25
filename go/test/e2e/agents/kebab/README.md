# Kebab Agent

This agent can be used to test KAgent BYO agent with ADK.

1. Build the agent image

```bash
docker build . --push -t localhost:5001/kebab:latest
```

2. Deploy the agent

```bash
kubectl apply -f agent.yaml
```

# Run manually

You can run the agent manually for testing purposes. Make sure you have Python 3.11+ installed.

```bash
cd go/test/e2e/agents/kebab
docker run --rm \
  -e KAGENT_URL=http://localhost:8083 \
  -e KAGENT_NAME=kebab-agent \
  -e KAGENT_NAMESPACE=kagent \
  --net=host \
  localhost:5001/kebab:latest
```