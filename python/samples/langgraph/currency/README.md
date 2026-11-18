# Currency LangGraph Agent

This is a currency LangGraph agent that demonstrates KAgent integration with controller-backed A2A task history.

## Features

- Currency conversion agent using OpenAI
- LangGraph state management with KAgent checkpointer
- A2A protocol compatibility
- A2A task persistence through controller gRPC
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

4. Configure a BYO `Harness` with this image and create an `Agent` that references it.
   Send messages to the Agent through the [A2A gateway](../../../../docs/architecture/a2a-transports.md).

## Architecture

This agent demonstrates:

- **StateGraph**: Simple conversation flow with one node
- **SqliteSaver**: Stores conversation state in a local SQLite file
- **A2A Integration**: Compatible with KAgent's agent-to-agent protocol
- **Streaming**: Real-time response streaming via A2A events

The agent stores conversation history in `KAGENT_CHECKPOINT_DB` (default: `/tmp/currency-checkpoints.sqlite`). For checkpoints that survive Actor replacement, set the variable to a path in the Actor’s durable directory under `/data`.

## Configuration

The agent can be configured via environment variables:

- `OPENAI_API_KEY`: Required for OpenAI API access

The runtime configuration selects the A2A gRPC address (default `[::]:80`). Substrate probes `GET /readyz` on port `8081`.

## Tracing (OTel)

High-level options for tracing this sample:

- **OpenTelemetry → Jaeger (or any OTLP backend)**
  - Already wired by `kagent-core` when enabled.
  - Set:
    ```bash
    export OTEL_TRACES_EXPORTER=otlp
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

### Runtime configuration file

Managed entry points call `materialize_from_bootstrap("/config")` from `kagent.core`
to fetch and materialize the controller bundle before `KAgentConfig.from_file()`.
Provider credentials and standard `OTEL_*` overrides remain environment inputs.
