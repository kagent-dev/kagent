# Rough Idea

## V2:

Use Temporal as a durable Agent workflow executor.

Replace or augment the current agent execution model in kagent with Temporal workflows, providing durability, retryability, and observability for long-running agent tasks. Temporal activities would wrap individual agent steps (tool calls, LLM invocations, MCP interactions), while workflows orchestrate the overall agent execution flow. This enables reliable execution of complex, multi-step agent tasks that can survive process restarts, handle failures gracefully, and provide visibility into execution state.

## V2:

Fix ERROR Activity error. Namespace default TaskQueue agent-istio-agent WorkerID 1@istio-agent-7f7b9b5bdf-hp29g@ WorkflowID agent- 
kagent__NS__istio_agent-ctx-24cc9c50-1804-49c5-8c06-0104f9ef30bc RunID ed3ab042-9579-4eb1-8771-23cbb94b41d7 ActivityType LLMInvokeActivity Attempt
Error model invoker is not configured

1. Use temporal Namespace same as Kuberentes namespace.
2. Use more friendly name for task queue and worker.
3. Use more friendly name for workflow and activity.