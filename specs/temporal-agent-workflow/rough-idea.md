# Rough Idea

Use Temporal as a durable Agent workflow executor.

Replace or augment the current agent execution model in kagent with Temporal workflows, providing durability, retryability, and observability for long-running agent tasks. Temporal activities would wrap individual agent steps (tool calls, LLM invocations, MCP interactions), while workflows orchestrate the overall agent execution flow. This enables reliable execution of complex, multi-step agent tasks that can survive process restarts, handle failures gracefully, and provide visibility into execution state.
