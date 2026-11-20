# Poem Generation Flow Example

This sample demonstrates how to use the `kagent-crewai` toolkit to run a CrewAI flow as a KAgent-compatible A2A service.

This example is generated directly from the `crewai create flow poem_flow` command.

If you wish to use the memory persistence integration with KAgent, edit `poem_flow.py` and set `@persist()` on the flow or methods you want to persist.

## Quick Start

1. **Build the agent image**:

   Run the poem-flow-sample target from the top-level Python directory.

   ```bash
   make poem-flow-sample
   ```

   This will build the Docker image and push it to your local registry.

2. **Create secrets for API keys**:

   ```bash
   kubectl create secret generic kagent-openai -n kagent \
     --from-literal=OPENAI_API_KEY=$OPENAI_API_KEY \
     --dry-run=client -o yaml | kubectl apply -f -
   ```

3. Configure a BYO `Harness` with this image and create an `Agent` that references it.
   Send messages to the Agent through the [A2A gateway](../../../../docs/architecture/a2a-transports.md).

When interacting with the agent, you do not need to provide any input because the design of the flow does not take in user input for its tasks.

## Configuration

The agent can be configured via environment variables:

- `GEMINI_API_KEY`: Required for LLM access

The runtime configuration selects the A2A gRPC address (default `[::]:80`). Substrate probes `GET /readyz` on port `8081`.

### Runtime configuration file

Managed entry points call `materialize_from_bootstrap("/config")` from `kagent.core`
to fetch and materialize the controller bundle before `KAgentConfig.from_file()`.
Provider credentials and standard `OTEL_*` overrides remain environment inputs.
