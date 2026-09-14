# Currency LangGraph Agent

This sample serves a currency-conversion LangGraph agent over A2A. LangGraph conversation state is checkpointed to a local SQLite file.

## Features

- Currency conversion using OpenAI and the Frankfurter API
- LangGraph state checkpointing with `SqliteSaver`
- A2A protocol support
- Streaming responses

## Deployment

The repository validates the packaged module import, ASGI application construction, and `GET /health` response. It does not validate the `main()` process lifecycle, port binding, container startup, OpenAI-backed execution, A2A requests, or checkpoint reuse after restart. This sample therefore has no documented end-to-end runtime command yet.

## Architecture

This agent demonstrates:

- **StateGraph**: A ReAct conversation graph with a currency tool
- **SqliteSaver**: Stores LangGraph conversation state in a local SQLite file
- **A2A Integration**: Serves the graph through KAgent's A2A application wrapper
- **Streaming**: Emits graph execution updates as A2A events

The agent stores LangGraph checkpoints in `KAGENT_CHECKPOINT_DB` (default: `/tmp/currency-checkpoints.sqlite`). Mount a persistent volume and point the variable there to retain checkpoints across pod replacement. A2A task tracking inside the agent process remains in memory.

## Configuration

- `OPENAI_API_KEY`: Required for OpenAI API access
- `KAGENT_API_URL`: Required by `KAgentConfig`; not used for outbound control-plane calls by this wrapper
- `KAGENT_GATEWAY_URL`: Required by `KAgentConfig`; not used for outbound gateway calls by this wrapper
- `KAGENT_NAME`: Required agent name
- `KAGENT_NAMESPACE`: Required agent namespace
- `KAGENT_CHECKPOINT_DB`: SQLite checkpoint path (default: `/tmp/currency-checkpoints.sqlite`)
- `PORT`: Server port (default: 8080)
- `HOST`: Server host (default: 0.0.0.0)

## Tracing (OTel)

High-level options for tracing this sample:

- **OpenTelemetry → Jaeger (or any OTLP backend)**
  - Already wired by `kagent-core` when enabled.
  - Set:
    ```bash
    export OTEL_TRACING_ENABLED=true
    export LANGSMITH_TRACING=true
    export LANGSMITH_OTEL_ENABLED=true
    export LANGSMITH_WORKSPACE_ID=<workspace-id>
    export LANGSMITH_ENDPOINT=http://<any-otlp-compatible-backend>:4317
    export OTEL_EXPORTER_OTLP_ENDPOINT=http://<any-otlp-compatible-backend>:4317
    ```
  - You should see logs like "Enabling tracing" and "Trace endpoint: ..." at startup.

- **Instrumenting tools**
  - If you create custom tools, decorate them with the LangSmith SDK's `@traceable` decorator; this sample shows it for the exchange-rate tool.

References:

- LangSmith SDK: https://github.com/langchain-ai/langsmith-sdk
- Trace with OpenTelemetry: https://docs.langchain.com/langsmith/trace-with-opentelemetry
