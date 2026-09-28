---
name: kagent
description: >
  Use kagent agents and standalone sandboxes. Use when configuring, invoking, sharing, or
  troubleshooting kagent agents, or using available kagent sandboxes as temporary workspaces
  to run scripts, test code, process files, and collect artifacts for an agent's own tasks.
---

# kagent user guide

kagent provides agent conversations through Sessions and A2A, and standalone
scratch environments through Sandboxes. Both use Substrate. Prefer the connected
deployment's tool schemas and current API over commands from earlier releases.

## Work with an agent

Use `kagent agent --help` to discover the installed agent workflow:

- `kagent agent list` and `kagent agent get NAME` discover agent definitions.
  `kagent agent template list` and `kagent agent template get NAME` inspect
  reusable AgentTemplates. These catalog commands currently require Kubernetes
  access and use the selected namespace.
- `kagent agent session create --agent NAME --request-id REQUEST_ID` creates a
  conversation. Generate and retain a request ID for retries, and keep the
  returned Session ID. Use
  `kagent agent session list` to list your conversations and
  `kagent agent session get ID` to inspect one.
- `kagent agent invoke --session ID --task "TASK"` invokes an existing
  conversation through A2A; `--stream` streams the response. Invocation does not
  create a Session automatically.
- `kagent agent session delete ID` deletes the conversation. It does not delete
  the agent definition. `kagent apply -f FILE` creates or updates an Agent or
  AgentTemplate from a manifest.

List commands accept `--page-size` and `--page-token`; get commands require one
name or ID. Session operations use the configured kagent API connection and
identity. Invocation also uses the configured gateway connection.

## Use a sandbox for your task

When a task benefits from an isolated tools environment, use available kagent
sandbox tools to execute scripts, check code, transform supplied data, or generate
artifacts. Choose an existing prepared SandboxTemplate with the needed programs.
A Sandbox can be used directly by the current agent; creating a kagent Agent or
Session is unnecessary for this workflow.

Read [Working in sandboxes](references/sandboxes.md) before using them. It covers
CLI commands, exact MCP inputs, a worked example, recovery, and agent setup.

- When a shell and configured kagent CLI are available, use `kagent sandbox` for
  execution and file transfer. It handles bytes, output polling, and exit codes
  outside the model context. Check `kagent sandbox --help` for installed support.
- Otherwise use the authenticated kagent `/mcp` connection already available to
  you. Discover its tools and, if the client supports prompts, retrieve
  `sandbox-task` for a task or `sandbox-recovery` for an interrupted sandbox.
- Reuse a sandbox associated with the current task when its environment and
  remaining lifetime fit. Keep its ID, creation request ID, and artifact paths.
- Sandboxes currently have no allowed egress destinations. Use tools and
  dependencies already in the image, and transfer inputs through the file API.
  Plan around this before choosing work that needs downloads or remote services.
- Retrieve useful results before deleting a sandbox created for the task. If it
  needs to remain available, report its ID, expiration, and remaining artifacts.
- If no suitable connection or template is available, explain what is missing
  and continue work possible with available tools. Setting up a cluster or
  changing shared templates belongs to a requested setup task.

## Target API

- `Agent` is an `api.kagent.dev/v1alpha3` CRD pairing a template and Harness, each inline or referenced. It owns readiness and revision selection.
- `Harness` is an `api.kagent.dev/v1alpha3` CRD describing a supported runtime adapter. The release-blocking adapters are kagent, Codex, and Claude.
- `AgentTemplate` is an `api.kagent.dev/v1alpha3` CRD describing prompts, models, skills, plugins, MCP tools, and template-backed subagent tools (`tools[].subAgent`).
- `Session` is a PostgreSQL-backed gRPC resource representing a conversation and its runtime lifecycle. Use the returned `context_id` for A2A interactions.
- A2A owns interaction and task history; Session APIs own lifecycle, metadata, and sharing. Checkpoint APIs own checkpoints and Session forks.
- `SandboxTemplate` is an `api.kagent.dev/v1alpha3` CRD defining a tools image and environment. Preparing a template does not create a Sandbox.
- `Sandbox` is an owner-scoped PostgreSQL resource exposed by gRPC and MCP for process/file work. Its lifetime and files are independent of Sessions.
- Substrate is the only compute backend.

## Guidance rules

1. Inspect available tool schemas for the connected deployment. When repository source is available, use `docs/architecture` and the implementation to verify behavior.
2. Verify CRDs from `go/api/v1alpha3` and generated manifests, protobuf APIs from `proto`, MCP inputs from `go/core/internal/mcp`, and CLI behavior from command help or source.
3. Describe planned behavior as planned until its implementation has landed.
4. Do not invent compatibility paths, migration procedures, fields, commands, or endpoints that are absent from the new API.
5. Use upstream A2A operations for agent interaction and history, Session/Checkpoint APIs for conversation lifecycle, and Sandbox tools for standalone commands and files.

## Stable design constraints

- One Session pins a compiled revision of one Agent, including its template tree.
- AgentTemplate references are same-namespace.
- Shared children (`subAgent.templateRef`) run inside their parent runtime. Dedicated bindings (`subAgent.agentRef`) are accepted by the API but currently rejected during compilation.
- Runtime state lives in DurableDir and must survive suspend/resume.
- Harness and SandboxTemplate own their respective runtime configuration. AgentTemplate describes behavior; do not add infrastructure fields to it.
- Actor identities and private runtime endpoints are implementation details.

For a workflow that has not landed, identify the missing capability rather than
falling back to an earlier API or inventing a roadmap link.
