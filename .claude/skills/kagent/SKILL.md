---
name: kagent
description: >
  Guide for using kagent's new Harness, AgentTemplate, AgentInstance, and A2A APIs. Use when a user asks
  how to configure, run, invoke, share, or troubleshoot kagent agents. The new API is still landing, so
  verify implemented commands and schemas in the repository before presenting them as available.
---

# kagent user guide

kagent is moving to a Substrate-backed API. Do not reuse commands, manifests, or concepts from earlier releases. The implementation roadmap is `docs/plans/api-v2-execution-plan.md`.

## Target API

- `Harness` is a `kagent.dev/v1alpha3` CRD describing a supported runtime adapter. The release-blocking adapters are kagent, Codex, and Claude.
- `AgentTemplate` is a `kagent.dev/v1alpha3` CRD describing prompts, models, skills, plugins, MCP tools, and other AgentTemplate tools.
- `AgentInstance` is a PostgreSQL-backed gRPC resource representing one runnable rooted template tree and one A2A context.
- `A2A context_id` equals the AgentInstance ID. A2A owns interaction and task history; AgentInstance APIs own lifecycle, metadata, and sharing.
- Substrate is the only compute backend.

## Guidance rules

1. Check the roadmap milestone and repository implementation before answering with exact syntax.
2. Verify CRDs from `go/api/v1alpha3` and generated manifests, protobuf APIs from `proto`, and CLI behavior from command help or source.
3. Describe planned behavior as planned until its roadmap PR has landed.
4. Do not invent compatibility paths, migration procedures, fields, commands, or endpoints that are absent from the new API.
5. Prefer upstream A2A operations for interaction and history. Use AgentInstance APIs for create, get, list, suspend, resume, delete, checkpoint, fork, and sharing as those services land.

## Stable design constraints

- One AgentInstance owns one rooted AgentTemplate tree.
- AgentTemplate references are same-namespace.
- Shared children run inside their parent runtime; Dedicated children use private, binding-scoped invocation.
- Runtime state lives in DurableDir and must survive suspend/resume.
- Public APIs do not expose Kubernetes scheduling, service accounts, workload deployments, arbitrary runtime containers, channels, or profiles.
- Actor identities and private runtime endpoints are implementation details.

Until a user-facing workflow lands, say that it is not available yet and point to the corresponding roadmap PR rather than falling back to an earlier workflow.
