# Basic Agent

This is a basic agent that can used to test KAgent BYO agent with ADK.

1. Build the agent image

```bash
docker build  . --push -t localhost:5001/my-byo:latest
```

Run the image through a BYO `Harness` and matching `AgentTemplate`; see the
API v2 examples and E2E fixtures for the current resource shape.
