import faulthandler
from typing import Union

from a2a.types import AgentCard
from google.protobuf.json_format import ParseDict
from kagent.core import AsyncControllerClient, KAgentConfig, bootstrap
from kagent.core.a2a import A2AServer, KAgentRequestContextBuilder
from kagent.core.a2a._task_store import KAgentRequestHandler, KAgentTaskStore
from kagent.core.telemetry import conv

from crewai import Crew, Flow

from ._executor import CrewAIAgentExecutor, CrewAIAgentExecutorConfig


class KAgentApp:
    def __init__(
        self,
        *,
        crew: Union[Crew, Flow],
        agent_card: AgentCard | dict,
        config: KAgentConfig | None = None,
        executor_config: CrewAIAgentExecutorConfig | None = None,
        tracing: bool = True,
    ):
        self._crew = crew
        self.agent_card = ParseDict(agent_card, AgentCard()) if isinstance(agent_card, dict) else agent_card
        self.config = config or KAgentConfig.from_file()
        self.executor_config = executor_config or CrewAIAgentExecutorConfig()
        self.tracing = tracing

    def build(self) -> A2AServer:
        agent_executor = CrewAIAgentExecutor(
            crew=self._crew,
            app_name=self.config.app_name,
            config=self.executor_config,
        )

        controller = AsyncControllerClient(self.config.api_url)
        task_store = KAgentTaskStore(controller)
        request_context_builder = KAgentRequestContextBuilder(task_store=task_store)
        request_handler = KAgentRequestHandler(
            agent_executor=agent_executor,
            task_store=task_store,
            agent_card=self.agent_card,
            request_context_builder=request_context_builder,
        )

        faulthandler.enable()

        if self.tracing:
            bootstrap(conv.KagentRuntimeValues.CREWAI, fallback_name=self.agent_card.name, config=self.config.runtime)

        return A2AServer(request_handler, self.config.runtime, controller)
