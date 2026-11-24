# Basic Agent

This is a basic agent that can used to test KAgent BYO agent with ADK.

1. Build the agent image

```bash
docker build  . --push -t localhost:5001/my-byo:latest
```

Configure a BYO Harness with this image, create an Agent that references the
Harness, and send messages through the [controller gateway](../../../../docs/architecture/a2a-transports.md).
