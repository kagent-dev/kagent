# HITL Tools LangGraph Agent

This sample serves a LangGraph workflow with human-in-the-loop tool approval
through `kagent-langgraph`'s A2A application wrapper.

## Local Startup

From `python/`, set the configuration required by `KAgentConfig` and the OpenAI
API key, then run the installed entry point:

```bash
export KAGENT_API_URL=http://localhost:8083
export KAGENT_GATEWAY_URL=http://localhost:8083
export KAGENT_NAME=hitl-tools
export KAGENT_NAMESPACE=default
export OPENAI_API_KEY=your-api-key
uv run --package hitl-tools hitl-tools
```

The wrapper requires the Kagent configuration values but does not use the API or
gateway URLs for outbound connections. `GET /health` is available on port 8080.

## Validation

The repository validates package importability, ASGI application construction,
and the health endpoint. It does not validate container startup, live A2A
requests, OpenAI-backed execution, deployment, or durable checkpoint behavior.
