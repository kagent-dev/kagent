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

4. Run the image through a BYO `Harness` and matching `AgentTemplate`; see the
   current examples and E2E fixtures for the resource shape.

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
   export OPENAI_API_KEY="sk-..."
   export SERPER_API_KEY="..."
   ```

3. **Run the agent server**:

   From the main Python directory:

   ```bash
   uv run research-crew
   ```

   Or from the sample directory:

   ```bash
   cd samples/crewai/research-crew
   uv run research-crew
   ```

## Configuration

The agent can be configured via environment variables:

- `OPENAI_API_KEY`: Required for LLM access
- `SERPER_API_KEY`: Required for web search functionality
- `PORT`: Server port (default: 8080)
- `HOST`: Server host (default: 0.0.0.0)

### Runtime configuration file

Managed runtimes receive `/config/config.json` from the controller. For a standalone
process, create that file and load it with `KAgentConfig.from_file()`:

```json
{"runtime":{"control_plane_url":"http://localhost:8083","gateway_url":"http://localhost:8083","name":"my-agent","namespace":"default","log_level":"info"}}
```

Provider credentials and standard `OTEL_*` overrides remain environment inputs.
