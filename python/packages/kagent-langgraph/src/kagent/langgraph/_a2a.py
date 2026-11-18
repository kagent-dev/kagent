"""KAgent LangGraph A2A Server Integration.

This module provides the main KAgentApp class that builds a gRPC server
with A2A protocol support for LangGraph workflows.
"""

import faulthandler

from a2a.types import AgentCard
from google.protobuf.json_format import ParseDict
from kagent.core import AsyncControllerClient, KAgentConfig, bootstrap
from kagent.core.a2a import (
    KAgentRequestContextBuilder,
    attach_hitl_agent_extension,
)

# --- Configure Logging ---
from kagent.core.a2a._runtime import A2AServer
from kagent.core.a2a._task_store import KAgentRequestHandler, KAgentTaskStore
from kagent.core.telemetry import conv

from langgraph.graph.state import CompiledStateGraph

from ._executor import LangGraphAgentExecutor, LangGraphAgentExecutorConfig


class KAgentApp:
    """Main application class for LangGraph + KAgent integration.

    This class builds a gRPC server with A2A protocol support,
    using LangGraph for agent execution and KAgent for state persistence.
    """

    def __init__(
        self,
        *,
        graph: CompiledStateGraph,
        agent_card: AgentCard | dict,
        config: KAgentConfig,
        executor_config: LangGraphAgentExecutorConfig | None = None,
        tracing: bool = True,
    ):
        """Initialize the KAgent application.

        Args:
            graph: Pre-compiled LangGraph
            agent_card: Agent card configuration for A2A protocol
            config: KAgent configuration
            executor_config: Optional executor configuration
            tracing: Enable OpenTelemetry via kagent.core.telemetry

        """
        self._graph = graph
        self.agent_card = ParseDict(agent_card, AgentCard()) if isinstance(agent_card, dict) else agent_card
        self.config = config

        self.executor_config = executor_config or LangGraphAgentExecutorConfig()
        self._enable_tracing = tracing

    def build(self) -> A2AServer:
        attach_hitl_agent_extension(self.agent_card)
        # Create agent executor
        agent_executor = LangGraphAgentExecutor(
            graph=self._graph,
            app_name=self.config.app_name,
            config=self.executor_config,
        )

        # Create task store
        controller = AsyncControllerClient(self.config.api_url)
        task_store = KAgentTaskStore(controller)

        # Create request context builder
        request_context_builder = KAgentRequestContextBuilder(task_store=task_store)

        # Create request handler
        request_handler = KAgentRequestHandler(
            agent_executor=agent_executor,
            task_store=task_store,
            agent_card=self.agent_card,
            request_context_builder=request_context_builder,
        )

        # Enable fault handler for debugging
        faulthandler.enable()

        if self._enable_tracing:
            bootstrap(
                conv.KagentRuntimeValues.LANGGRAPH, fallback_name=self.agent_card.name, config=self.config.runtime
            )

        return A2AServer(request_handler, self.config.runtime, controller)
