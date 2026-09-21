# Currency LangGraph Agent

This sample serves a currency-conversion LangGraph agent over A2A. LangGraph conversation state is checkpointed to a local SQLite file.

## Features

- Currency conversion using OpenAI and the Frankfurter API
- LangGraph state checkpointing with `SqliteSaver`
- A2A protocol support
- Streaming responses

## Deployment

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
   current examples and E2E fixtures for the resource shape.

## Local Development

1. Install dependencies:

```bash
uv sync
```

2. Set environment variables:

```bash
export OPENAI_API_KEY=your_api_key_here
# Load process settings from /config/config.json (see Configuration).
```

3. Run the agent server:

```bash
uv run currency
```

4. Test the agent:

```bash
uv run currency test
```

The startup regression test invokes `main()` with the server runner mocked and
checks `GET /health`. It does not cover port binding, container startup,
OpenAI-backed execution, A2A requests, or checkpoint reuse after restart.

## Architecture

This agent demonstrates:

- **StateGraph**: A ReAct conversation graph with a currency tool
- **SqliteSaver**: Stores LangGraph conversation state in a local SQLite file
- **A2A Integration**: Serves the graph through KAgent's A2A application wrapper
- **Streaming**: Emits graph execution updates as A2A events

The agent stores LangGraph checkpoints in `KAGENT_CHECKPOINT_DB` (default: `/tmp/currency-checkpoints.sqlite`). Mount a persistent volume and point the variable there to retain checkpoints across pod replacement. A2A task tracking inside the agent process remains in memory.

## Configuration

- `OPENAI_API_KEY`: Required for OpenAI API access
- `KAGENT_CHECKPOINT_DB`: SQLite checkpoint path (default: `/tmp/currency-checkpoints.sqlite`)
- `PORT`: Server port (default: 8080)
- `HOST`: Server host (default: 0.0.0.0)

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

Managed runtimes receive `/config/config.json` from the controller. For a standalone
process, create that file and load it with `KAgentConfig.from_file()`:

```json
{"runtime":{"control_plane_url":"http://localhost:8083","gateway_url":"http://localhost:8083","name":"my-agent","namespace":"default","log_level":"info"}}
```

Provider credentials and standard `OTEL_*` overrides remain environment inputs.
