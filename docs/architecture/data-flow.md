# End-to-End Data Flow

This document traces a user message through kagent.

## Message flow

\`\`\`
UI
  → controller HTTP server
  → SandboxAgent A2A transport
  → Agent Substrate actor
  → Python or Go ADK runtime
  → LLM and MCP tool servers
  → streamed A2A events back through the controller
  → UI
\`\`\`

The UI sends standard A2A JSON-RPC through the SandboxAgent route. The controller
creates or resumes the database conversation and session, resolves the current
Substrate ActorTemplate, and forwards the request to the session actor. There is
no per-agent Kubernetes Deployment or Service.

The ADK runtime builds the LLM request from the system prompt, session history,
and compiled tools. Tool calls are sent to MCP servers and their results are fed
back to the LLM. ADK events are converted to A2A events and streamed to the UI.

## Configuration flow

\`\`\`
SandboxAgent CRD
  → SandboxAgentController
  → translator resolves prompt, model credentials, tools, and agent card
  → Kubernetes config Secret + Substrate ActorTemplate
  → actor materializes config from Secret-backed environment variables
  → ADK runtime
\`\`\`

The ActorTemplate contains the runtime image, command, environment, readiness
check, worker-pool selector, snapshot policy, and DurableDir session volume.
Kubernetes pod scheduling and service-account fields are not part of the
SandboxAgent workload contract.

See [controller-reconciliation.md](controller-reconciliation.md) for the
reconciliation flow.
