# Research Crew Sample

This sample demonstrates how to use the `kagent-crewai` toolkit to run a CrewAI crew as a KAgent-compatible A2A service.

It follows the standard CrewAI project structure and developer experience, allowing you to define your agents and tasks in Python.

`KAgentApp` does not configure or persist CrewAI memory. Configure CrewAI memory and its backing storage explicitly when needed.

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
   API v2 examples and E2E fixtures for the current resource shape. The sample
   does not configure Kagent-backed task history or CrewAI memory persistence.

## Local Development

1. **Install dependencies from the parent Python directory**:

   Navigate to the main Python directory and install all workspace dependencies:

   ```bash
   cd ../../..  # Go to /kagent/python
   uv sync --all-extras
   ```

2. **Set environment variables**:

   ```bash
   export KAGENT_API_URL=http://localhost:8083
   export KAGENT_GATEWAY_URL=http://localhost:8083
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
- `KAGENT_API_URL`: Required by `KAgentConfig`; not used by this wrapper for outbound control-plane calls
- `KAGENT_GATEWAY_URL`: Required by `KAgentConfig`; not used by this wrapper for outbound gateway calls
- `PORT`: Server port (default: 8080)
- `HOST`: Server host (default: 0.0.0.0)
