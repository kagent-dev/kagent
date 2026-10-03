# Kebab LangGraph Agent

This sample serves a minimal LangGraph workflow through `kagent-langgraph`'s A2A
application wrapper.

## Local Startup

From `python/`, set the configuration required by `KAgentConfig` and run the
installed entry point:

```bash
export KAGENT_API_URL=http://localhost:8083
export KAGENT_GATEWAY_URL=http://localhost:8083
export KAGENT_NAME=kebab
export KAGENT_NAMESPACE=default
uv run --package kebab kebab
```

The wrapper requires the Kagent configuration values but does not use the API or
gateway URLs for outbound connections. `GET /health` is available on port 8080.

## Validation

The repository validates package importability, ASGI application construction,
and the health endpoint. It does not validate container startup, live A2A
requests, deployment, or durable checkpoint behavior.
