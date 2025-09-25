# Research Crew Sample

This sample demonstrates how to use the `kagent-crewai` toolkit to run a CrewAI crew as a KAgent-compatible A2A service.

It follows the standard CrewAI project structure and developer experience, allowing you to define your agents and tasks in Python.

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
   kubectl create secret generic kagent-google -n kagent \
     --from-literal=GOOGLE_API_KEY=$GOOGLE_API_KEY \
     --dry-run=client -o yaml | kubectl apply -f -

   kubectl create secret generic kagent-serper -n kagent \
     --from-literal=SERPER_API_KEY=$SERPER_API_KEY \
     --dry-run=client -o yaml | kubectl apply -f -
   ```

4. **Deploy the agent**:

   ```bash
   kubectl apply -f agent.yaml
   ```

## Local Development

1. **Install dependencies from the parent Python directory**:

   Navigate to the main Python directory and install all workspace dependencies:

   ```bash
   cd ../../..  # Go to /kagent/python
   uv sync --all-extras
   ```

2. **Set environment variables**:

   ```bash
   export KAGENT_URL=http://localhost:8080
   export GEMINI_API_KEY="sk-..."
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
- `KAGENT_URL`: KAgent server URL (default: http://localhost:8080)
- `PORT`: Server port (default: 8080)
- `HOST`: Server host (default: 0.0.0.0)
