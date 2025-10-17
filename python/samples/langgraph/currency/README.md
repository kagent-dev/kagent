# Currency LangGraph Agent

This is a currency LangGraph agent that demonstrates KAgent integration with session persistence via REST API.

## Features

- Currency conversion agent using Google Gemini
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

3. Create a secret with the Google API key:

```bash
kubectl create secret generic kagent-google -n kagent \
  --from-literal=GOOGLE_API_KEY=$GOOGLE_API_KEY \
  --dry-run=client -o yaml | kubectl apply -f -
```

4. Deploy the agent:

```bash
kubectl apply -f agent.yaml
```

## Local Development

1. Install dependencies:

```bash
uv sync
```

2. Set environment variables:

```bash
export GOOGLE_API_KEY=your_api_key_here
export KAGENT_URL=http://localhost:8080
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
- **KAgentCheckpointer**: Persists conversation state to KAgent sessions
- **A2A Integration**: Compatible with KAgent's agent-to-agent protocol
- **Streaming**: Real-time response streaming via A2A events

The agent maintains conversation history across sessions using the KAgent REST API for persistence.

## Configuration

The agent can be configured via environment variables:

- `GOOGLE_API_KEY`: Required for Gemini API access
- `KAGENT_URL`: KAgent server URL (default: http://localhost:8080)
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
