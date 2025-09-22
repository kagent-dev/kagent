# Poem Generation Flow Example

This sample demonstrates how to use the `kagent-crewai` toolkit to run a CrewAI flow as a KAgent-compatible A2A service.

This example is generated directly from the `crewai create flow poem_flow` command.

## Quick Start

1. **Build the agent image**:

   Run the poem-flow-sample target from the top-level Python directory.

   ```bash
   make poem-flow-sample
   ```

   This will build the Docker image and push it to your local registry.

2. **Create secrets for API keys**:

   ```bash
   kubectl create secret generic kagent-google -n kagent \
     --from-literal=GOOGLE_API_KEY=$GOOGLE_API_KEY \
     --dry-run=client -o yaml | kubectl apply -f -
   ```

3. **Deploy the agent**:

   ```bash
   kubectl apply -f agent.yaml
   ```

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
   export KAGENT_URL=http://localhost:8080
   export GEMINI_API_KEY="sk-..."
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
- `KAGENT_URL`: KAgent server URL (default: http://localhost:8080)
- `PORT`: Server port (default: 8080)
- `HOST`: Server host (default: 0.0.0.0)
