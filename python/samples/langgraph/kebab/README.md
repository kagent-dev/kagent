# Kebab LangGraph Agent

This sample serves a minimal LangGraph workflow through `kagent-langgraph`'s A2A
application wrapper.

## Local Startup

From `python/`, provide `/config/config.json` and run the installed entry point:

```bash
uv run --package kebab kebab
```

The runtime file must define `control_plane_url`, `gateway_url`, `name`, and
`namespace`. The wrapper uses the control-plane endpoint for public A2A task
history. `GET /health` is available on port 8080.

## Validation

The repository validates package importability, ASGI application construction,
and the health endpoint. It does not validate container startup, live A2A
requests, deployment, or durable checkpoint behavior.
