# Architecture

Kagent defines portable agent behavior with `AgentTemplate` and compiles it
through an admitted `Harness`. The resulting revision is applied to a Substrate
ActorTemplate. PostgreSQL-backed `AgentInstance` records own lifecycle and public
A2A contexts; they are not Kubernetes resources.

Detailed documents:

- [Human in the loop](human-in-the-loop.md)
- [Prompt templates](prompt-templates.md)
- [A2A agent tools](a2a-subagents.md)

The implementation roadmap and dependency graph live in
[the API v2 execution plan](../plans/api-v2-execution-plan.md).
