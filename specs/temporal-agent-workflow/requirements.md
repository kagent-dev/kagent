# Requirements

## Questions & Answers

**Q1:** Should the initial implementation target self-hosted Temporal (via temporal-helm chart with PostgreSQL) or Temporal Cloud? This affects deployment complexity, Helm chart design, and TLS configuration. The design currently proposes self-hosted first with a Cloud upgrade path -- does that match your preference?

**A1:** Self-hosted. Start with temporal-helm chart, PostgreSQL backend. Temporal Cloud is out of scope for now.

**Q2:** Should the Temporal worker run in-process within the existing agent pod (same binary, started alongside the A2A server), or as a separate Deployment/pod? In-process is simpler but couples worker lifecycle to the A2A server. Separate pods allow independent scaling but add deployment complexity.

**A2:** In-process, same binary as the A2A server. Worker starts alongside the A2A server in the agent pod.

**Q3:** For the LLM + tool loop inside the workflow -- should each individual LLM turn be a separate activity (enabling retry/resume at turn granularity), or should the entire `Agent.Run()` loop be a single activity (simpler but coarser recovery)? The design proposes per-turn activities for maximum durability, but the single-activity approach is much simpler to implement initially.

**A3:** Per-turn granularity. Each LLM invocation and each tool call is a separate activity for maximum durability and retry precision.

**Q4:** Should the feature gate be purely environment-variable based (`TEMPORAL_ENABLED=true`), or should it also be configurable per-agent via the Agent CRD spec (e.g., `spec.temporal.enabled: true`)? Per-agent control would allow mixed deployments where some agents use Temporal and others don't.

**A4:** Per-agent via CRD spec. `spec.temporal.enabled: true` on the Agent CRD controls whether that agent uses Temporal.

**Q5:** What database backend should the self-hosted Temporal server use -- PostgreSQL (recommended, matches kagent's production DB option) or SQLite (simpler for dev but not production-grade for Temporal)? And should it share kagent's existing database instance or have its own dedicated one?

**A5:** Both. SQLite for dev, PostgreSQL for production. Switchable via Helm values. Dedicated database instance (not shared with kagent).

**Q6:** Should HITL (Human-in-the-Loop) signal support be part of the initial implementation, or deferred to a later phase? The current design includes it in Step 7, but it touches the UI and adds complexity. Would you prefer to ship the core workflow execution first and add HITL signals after?

**A6:** HITL is required and must be part of the initial implementation. Not deferred.

**Q7:** For the task queue strategy -- should all agents share a single task queue (e.g., `agent-execution`), or should each agent get its own task queue (e.g., `agent-execution-{agentName}`)? Per-agent queues provide isolation (one agent's backlog doesn't block others) but require more Temporal resources.

**A7:** Per-agent task queues. Each agent gets its own queue named `agent-{agentName}` for isolation.

**Q8:** Should the Temporal UI be exposed alongside kagent's UI, or is it only for ops/debugging? If exposed, should it be accessible via the same ingress/port-forward, or separate? This affects the Helm chart and RBAC setup.

**A8:** Temporal UI should be exposed as a KAgent MCP plugin (dynamic plugin system). Proxied through kagent, not a separate ingress. Appears in the kagent sidebar via the plugin navigation system.

**Q9:** For streaming responses -- the current A2A server supports SSE streaming to the UI. When Temporal executes a workflow, should the client block until workflow completion (simpler), or should intermediate events (LLM tokens, tool progress) be streamed back to the client in real-time via Temporal queries or a side-channel?

**A9:** Streamed. Intermediate events (LLM tokens, tool progress) must be streamed back to the client in real-time. Not blocking until completion.

**Q10:** For the streaming mechanism specifically -- Temporal workflows don't natively support token-level streaming. Two options:
- **Temporal Query + polling**: Workflow updates a queryable state, client polls via SSE. Simple but adds latency.
- **Side-channel (e.g., Redis pub/sub or direct WebSocket)**: Activity streams tokens to a pub/sub topic, A2A server subscribes and forwards to client. Real-time but adds infrastructure dependency.

Which approach do you prefer?

**A10:** NATS as the side-channel. Activity publishes LLM tokens/tool progress to NATS subject (e.g., `agent.{agentName}.{sessionID}.stream`), A2A server subscribes and forwards to client via SSE. NATS is lightweight, CNCF-graduated, K8s-native, and adds minimal infrastructure overhead.

**Q11:** Should NATS be deployed as part of the kagent Helm chart (embedded), or expected as a pre-existing cluster dependency? Also, should NATS JetStream (persistent streams) be used for durability of streaming events, or is core NATS (fire-and-forget pub/sub) sufficient since the workflow history in Temporal is the source of truth?

**A11:** Embedded in kagent Helm chart. Core NATS with fire-and-forget pub/sub -- no JetStream needed. Temporal workflow history is the source of truth for execution state.

**Q12:** For child workflows (A2A multi-agent) -- should this be part of the initial scope, or deferred? It's Step 8 in the plan and adds complexity around cross-agent task queue routing and session management.

**A12:** Required in initial scope. A2A multi-agent calls must be child workflows. All tool calls (MCP) must be wrapped as individual Temporal activities. Full workflow composition from day one.

**Q13:** For observability -- should we add a Grafana dashboard for Temporal metrics (workflow latency, activity retries, queue depth) as part of the initial deliverable, or is the Temporal UI (exposed as MCP plugin per Q8) sufficient for monitoring initially?

**A13:** Temporal UI (as MCP plugin) is sufficient for initial monitoring. Grafana dashboards deferred.

**Q14:** Should there be a maximum workflow execution timeout (e.g., 1 hour, 24 hours)? Some agent tasks could potentially run very long with HITL waits. Should the timeout be configurable per-agent in the CRD spec?

**A14:** 48 hours default workflow execution timeout. Configurable per-agent in the CRD spec.
