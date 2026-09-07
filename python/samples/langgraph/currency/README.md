# Currency LangGraph Agent

This is a currency LangGraph agent that demonstrates KAgent integration with session persistence via REST API.

## Features

- Currency conversion agent using OpenAI
- LangGraph state management with KAgent checkpointer
- A2A protocol compatibility
- Session persistence via KAgent REST API
- Streaming responses

## Quick Start

1. Build the agent image:

Run the basic-langchain-sample target from the top-level Python directory.

```bash
make basic-langchain-sample
```

2. Push to local registry (if using one):

```bash
docker push localhost:5001/langgraph-currency:latest
```

3. Create a secret with the OpenAI API key:

```bash
kubectl create secret generic kagent-openai -n kagent \
  --from-literal=OPENAI_API_KEY=$OPENAI_API_KEY \
  --dry-run=client -o yaml | kubectl apply -f -
```

4. Run the image through a BYO `Harness` and matching `AgentTemplate`; see the
   API v2 examples and E2E fixtures for the current resource shape.

## Local Development

1. Install dependencies:

```bash
uv sync
```

2. Set environment variables:

```bash
export OPENAI_API_KEY=your_api_key_here
export KAGENT_API_URL=http://localhost:8083
export KAGENT_GATEWAY_URL=http://localhost:8083
```

3. Run the agent server:

```bash
uv run currency
```

4. Test the agent:

```bash
uv run currency test
```

## Architecture

This agent demonstrates:

- **StateGraph**: Simple conversation flow with one node
- **SqliteSaver**: Stores conversation state in a local SQLite file
- **A2A Integration**: Compatible with KAgent's agent-to-agent protocol
- **Streaming**: Real-time response streaming via A2A events

The agent stores conversation history in `KAGENT_CHECKPOINT_DB` (default: `/tmp/currency-checkpoints.sqlite`). Mount a persistent volume and point the variable there for persistence across pod replacement.

## Configuration

The agent can be configured via environment variables:

- `OPENAI_API_KEY`: Required for OpenAI API access
- `KAGENT_API_URL`: Required. KAgent control-plane API URL; locally, `http://localhost:8083`
- `KAGENT_GATEWAY_URL`: Required. KAgent A2A and MCP gateway URL; locally, `http://localhost:8083`
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
- LangSmith SDK: `https://github.com/langchain-ai/langsmith-sdk`
- Trace with OpenTelemetry: `https://docs.langchain.com/langsmith/trace-with-opentelemetry`
