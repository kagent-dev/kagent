---
name: kagent-mcp
description: >-
  Connect an MCP client to kagent's built-in Streamable HTTP endpoint and use
  its tools to list agents, create and invoke Sessions, checkpoint and fork
  them, and run work in standalone Sandboxes, plus the sandbox-task and
  sandbox-recovery prompts. Use when driving kagent agents or sandboxes from
  another agent over MCP.
---

# kagent over MCP

References: `docs/architecture/mcp.md`, `docs/architecture/sandboxes.md`,
`go/core/internal/mcp/`. For the CLI equivalents see the `kagent` skill.

## Connect

The controller serves an authenticated, stateless Streamable HTTP endpoint
at `/mcp` on port `8083`:

```bash
kubectl -n kagent port-forward svc/kagent-controller 8083:8083
# MCP URL: http://localhost:8083/mcp
```

Behind the OIDC proxy, `/mcp` is routed through the proxy and the client
must send the bearer token it forwards. Keep the controller's direct
listener private.

## Tools

| Area | Tools |
|------|-------|
| Agents | `list_agents` |
| Sessions | `create_session`, `list_sessions`, `get_session`, `delete_session`, `invoke_agent` |
| Checkpoints | `create_session_checkpoint`, `list_session_checkpoints`, `get_session_checkpoint`, `fork_session` |
| Tasks | `get_task`, `list_tasks` (MCP Tasks extension: `tasks/get`, `tasks/update`, `tasks/cancel`) |
| Sandboxes | `list_sandbox_templates`, `create_sandbox`, `get_sandbox`, `list_sandboxes`, `delete_sandbox` |
| Sandbox work | `write_sandbox_file`, `start_sandbox_process`, `get_sandbox_process`, `read_sandbox_outputs`, `read_sandbox_file` |

Read the server instructions and each tool's description; they carry the
current constraints.

## Agent conversations

1. `list_agents` to find the agent.
2. `create_session`, keep the Session ID.
3. `invoke_agent` with that Session. Invoking does not create a Session.
   Streaming clients get updates from the same durable A2A task; an
   `input-required` task continues with `tasks/update`.
4. Checkpoint before risky turns and `fork_session` to branch from one.

## Sandboxes

Prefer the `sandbox-task` prompt (`task`, optional `namespace` and
`template`) and `sandbox-recovery` (`sandbox_id`) when your client supports
prompts. Otherwise:

1. `list_sandbox_templates`, then `create_sandbox`.
2. Stage inputs with `write_sandbox_file` under `/data/workspace`
   (base64 transfer has size limits).
3. `start_sandbox_process`; poll `get_sandbox_process` and read output with
   `read_sandbox_outputs` using offsets. Processes are asynchronous.
4. Retrieve artifacts with `read_sandbox_file` before `delete_sandbox`.

Sandboxes have their own lifetime and currently no allowed egress, so do
not plan work that needs downloads. After an interruption, inspect the
sandbox before re-running commands to avoid duplicating work.
