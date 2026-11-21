# Research Crew Sample

This sample demonstrates how to use the `kagent-crewai` toolkit to run a CrewAI crew as a KAgent-compatible A2A service.

It follows the standard CrewAI project structure and developer experience, allowing you to define your agents and tasks in Python.

If you wish to use the memory persistence integration with KAgent, edit `crew.py` and set `memory=True` when creating the crew.

## Features

- Research crew with multiple specialized agents
- CrewAI orchestration with KAgent integration
- A2A protocol compatibility
- Web search capabilities via Serper API
- Multi-agent collaboration

## Quick Start

1. **Build the agent image**:

   Run the research-crew-sample target from the top-level Python directory.

   ```bash
   make research-crew-sample
   ```

2. **Push to local registry** (if using one):

   ```bash
   docker push localhost:5001/research-crew:latest
   ```

3. **Create secrets for API keys**:

   ```bash
   kubectl create secret generic kagent-openai -n kagent \
     --from-literal=OPENAI_API_KEY=$OPENAI_API_KEY \
     --dry-run=client -o yaml | kubectl apply -f -

   kubectl create secret generic kagent-serper -n kagent \
     --from-literal=SERPER_API_KEY=$SERPER_API_KEY \
     --dry-run=client -o yaml | kubectl apply -f -
   ```

4. Configure a BYO `Harness` with this image and create an `Agent` that references it.
   Send messages to the Agent through the [A2A gateway](../../../../docs/architecture/a2a-transports.md).

## Configuration

The agent can be configured via environment variables:

- `OPENAI_API_KEY`: Required for LLM access
- `SERPER_API_KEY`: Required for web search functionality

The runtime configuration selects the A2A gRPC address (default `[::]:80`). Substrate probes `GET /readyz` on port `8081`.

### Runtime configuration file

Managed entry points call `materialize_from_bootstrap("/config")` from `kagent.core`
to fetch and materialize the controller bundle before `KAgentConfig.from_file()`.
Provider credentials and standard `OTEL_*` overrides remain environment inputs.
