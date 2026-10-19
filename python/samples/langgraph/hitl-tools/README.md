# HITL Tools LangGraph Agent

This sample serves a LangGraph workflow with human-in-the-loop tool approval
through `kagent-langgraph`'s A2A application wrapper.

## Local Startup

From `python/`, provide `/config/config.json`, set the OpenAI API key, and run
the installed entry point:

```bash
export OPENAI_API_KEY=your-api-key
uv run --package hitl-tools hitl-tools
```

The runtime file must define `control_plane_url`, `gateway_url`, `name`, and
`namespace`. The wrapper uses the control-plane endpoint for public A2A task
history. `GET /health` is available on port 8080.

## Validation

The repository validates package importability, ASGI application construction,
and the health endpoint. It does not validate container startup, live A2A
requests, OpenAI-backed execution, deployment, or durable checkpoint behavior.
