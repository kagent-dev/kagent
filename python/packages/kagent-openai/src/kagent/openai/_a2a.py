"""KAgent OpenAI Agents SDK Application.

This module provides the main KAgentApp class for building gRPC servers
that integrate OpenAI Agents SDK with the A2A (Agent-to-Agent) protocol.
"""

from __future__ import annotations

import faulthandler
import logging
import os
from collections.abc import Callable

from a2a.types import AgentCard
from agents import Agent, set_default_openai_api, set_default_openai_client, set_tracing_disabled
from google.protobuf.json_format import ParseDict
from kagent.core import AsyncControllerClient, KAgentConfig, bootstrap, signal_enabled
from kagent.core.a2a import KAgentRequestContextBuilder
from kagent.core.a2a._runtime import A2AServer
from kagent.core.a2a._task_store import KAgentRequestHandler, KAgentTaskStore
from kagent.core.telemetry import conv
from opentelemetry.instrumentation.genai.openai_agents import OpenAIAgentsInstrumentor

from openai import AsyncOpenAI

from ._agent_executor import OpenAIAgentExecutor, OpenAIAgentExecutorConfig

logger = logging.getLogger(__name__)


def _configure_openai_client() -> None:
    """
    Configure the default OpenAI client to use OPENAI_API_BASE.
    """

    openai_api_base = os.getenv("OPENAI_API_BASE")
    api_key = os.getenv("OPENAI_API_KEY")
    if openai_api_base and api_key:
        # Create a custom AsyncOpenAI client with the base URL
        custom_client = AsyncOpenAI(
            base_url=openai_api_base,
            api_key=api_key,
        )

        # Set it as the default client for the OpenAI Agents SDK
        set_default_openai_client(custom_client)
        # By default it uses the OpenAI responses API but this is not supported for most other providers
        set_default_openai_api("chat_completions")
        logger.info(f"Configured OpenAI client with base URL: {openai_api_base}")


def _openai_agents_instrument_options(keep_native: bool = False) -> dict[str, dict[str, bool]]:
    """Export OpenAI Agents SDK traces through OpenTelemetry only.

    The SDK's built-in processor POSTs to a hardcoded https://api.openai.com/v1/traces/ingest
    using OPENAI_API_KEY, ignoring OPENAI_API_BASE, so agents behind an OpenAI-compatible
    gateway (Azure AI Foundry, LiteLLM, ...) send that gateway's key to OpenAI and get 401s.
    The instrumentor adds its processor alongside that one, and set_tracing_disabled(True)
    would silence the OpenTelemetry spans too, so drop the built-in processor instead.

    runtime.telemetry.openai_native_tracing keeps it for OpenAI platform keys.
    """
    if keep_native:
        logger.info("Keeping the OpenAI Agents SDK native trace exporter alongside OpenTelemetry")
    else:
        logger.info("Disabling the OpenAI Agents SDK native trace exporter; traces are exported via OpenTelemetry")

    if os.getenv("OPENAI_AGENTS_DISABLE_TRACING", "false").strip().lower() in ("true", "1"):
        logger.warning(
            "OPENAI_AGENTS_DISABLE_TRACING is set, which switches off the Agents SDK tracing that the "
            "OpenTelemetry instrumentation feeds on, so no agent spans will be exported. Unset it and rely "
            "on runtime.telemetry.openai_native_tracing=false (the default) to keep traces away from OpenAI."
        )
    return {"openai_agents": {"disable_openai_trace_export": not keep_native}}


class KAgentApp:
    """A2A gRPC server builder for OpenAI Agents SDK with KAgent integration."""

    def __init__(
        self,
        agent: Agent | Callable[[], Agent],
        agent_card: AgentCard | dict,
        config: KAgentConfig,
        executor_config: OpenAIAgentExecutorConfig | None = None,
        tracing: bool = True,
    ):
        """Initialize the KAgent application.

        Args:
            agent: OpenAI Agent instance or factory function
            agent_card: A2A agent card, either protobuf or plain dict form
            config: KAgent configuration
            executor_config: Optional executor configuration
        """
        self.agent = agent
        self.agent_card = ParseDict(agent_card, AgentCard()) if isinstance(agent_card, dict) else agent_card
        self.config = config
        self.executor_config = executor_config or OpenAIAgentExecutorConfig()
        self.tracing = tracing

    def build(self) -> A2AServer:
        """Build the controller-backed A2A gRPC server."""
        _configure_openai_client()

        agent_executor = OpenAIAgentExecutor(
            agent=self.agent,
            config=self.executor_config,
        )

        controller = AsyncControllerClient(self.config.api_url)
        task_store = KAgentTaskStore(controller)

        # Create request context builder and handler
        request_context_builder = KAgentRequestContextBuilder(task_store=task_store)
        request_handler = KAgentRequestHandler(
            agent_executor=agent_executor,
            task_store=task_store,
            agent_card=self.agent_card,
            request_context_builder=request_context_builder,
        )

        # Enable fault handler
        faulthandler.enable()

        if self.tracing:
            bootstrap(
                conv.KagentRuntimeValues.OPENAI_AGENTS,
                fallback_name=self.agent_card.name,
                config=self.config.runtime,
                instrument_options=_openai_agents_instrument_options(
                    self.config.runtime.telemetry.openai_native_tracing
                ),
            )
            if not signal_enabled("TRACES") or not OpenAIAgentsInstrumentor().is_instrumented_by_opentelemetry:
                set_tracing_disabled(True)

        return A2AServer(request_handler, self.config.runtime, controller)

    async def test(self, task: str) -> None:
        """Test the agent with a simple task.

        Args:
            task: The task/question to ask the agent
        """
        from agents.run import Runner

        # Resolve agent
        if callable(self.agent):
            agent = self.agent()
        else:
            agent = self.agent

        logger.info(f"\n>>> User Query: {task}")

        # Run the agent
        result = await Runner.run(agent, task)

        logger.info(f">>> Agent Response: {result.final_output}")
