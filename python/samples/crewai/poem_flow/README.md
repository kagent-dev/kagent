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

3. Run the image through a BYO `Harness` and matching `AgentTemplate`; see the
   current examples and E2E fixtures for the resource shape.

When interacting with the agent, you do not need to provide any input because the design of the flow does not take in user input for its tasks.

## Local Development

1. **Install dependencies from the parent Python directory**:

   Navigate to the main Python directory and install all workspace dependencies:

   ```bash
   cd ../../..  # Go to /kagent/python
   uv sync --all-extras
   ```

2. **Set environment variables**:

   ```bash
   # Load process settings from /config/config.json (see Configuration).
   export OPENAI_API_KEY="..."
   ```

3. **Run the agent server**:

   From the main Python directory:

   ```bash
   uv run poem-flow
   ```

   Or from the sample directory:

   ```bash
   cd samples/crewai/poem_flow
   uv run poem-flow
   ```

## Configuration

The agent can be configured via environment variables:

- `GEMINI_API_KEY`: Required for LLM access
- `PORT`: Server port (default: 8080)
- `HOST`: Server host (default: 0.0.0.0)

### Runtime configuration file

Managed runtimes receive `/config/config.json` from the controller. For a standalone
process, create that file and load it with `KAgentConfig.from_file()`:

```json
{"runtime":{"control_plane_url":"http://localhost:8083","gateway_url":"http://localhost:8083","name":"my-agent","namespace":"default","log_level":"info"}}
```

Provider credentials and standard `OTEL_*` overrides remain environment inputs.
