# Basic Agent

This is a basic agent that can be used to test a KAgent custom runtime with ADK.

1. Build the agent image

```bash
docker build  . --push -t localhost:5001/my-byo:latest
```

Run the image through a custom `Harness` and matching `AgentTemplate`; see the
current examples and E2E fixtures for the resource shape.
