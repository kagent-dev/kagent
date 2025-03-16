from typing import Any, AsyncGenerator, List, Mapping, Sequence

from autogen_agentchat.agents._base_chat_agent import BaseChatAgent
from autogen_agentchat.base import Response, TaskResult, Team
from autogen_agentchat.messages import (
    AgentEvent,
    BaseChatMessage,
    ChatMessage,
    HandoffMessage,
    ModelClientStreamingChunkEvent,
    TextMessage,
    ToolCallExecutionEvent,
    ToolCallRequestEvent,
    ToolCallSummaryMessage,
)
from autogen_agentchat.state import BaseState
from autogen_core import CancellationToken, Component, ComponentModel
from autogen_core.model_context import ChatCompletionContext, UnboundedChatCompletionContext
from autogen_core.models import (
    AssistantMessage,
    FunctionExecutionResultMessage,
)
from pydantic import BaseModel, Field
from typing_extensions import Self


class TaskAgentState(BaseState):
    """State for a Task agent."""

    inner_team_state: Mapping[str, Any] = Field(default_factory=dict)
    model_context_state: Mapping[str, Any] = Field(default_factory=dict)
    type: str = Field(default="TaskAgentState")


class TaskAgentConfig(BaseModel):
    """The declarative configuration for a TaskAgent."""

    name: str
    team: ComponentModel
    model_context: ComponentModel | None = None
    description: str | None = None


class TaskAgent(BaseChatAgent, Component[TaskAgentConfig]):
    """An agent that uses an inner team of agents to generate responses.

    Each time the agent's :meth:`on_messages` or :meth:`on_messages_stream`
    method is called, it runs the inner team of agents and then returns the
    final response. It will also reset the inner team by calling
    :meth:`Team.reset`.

    Args:
        name (str): The name of the agent.
        team (Team): The team of agents to use.
        model_context (ChatCompletionContext, optional): The model context to use for preparing responses.
        description (str, optional): The description of the agent.
    """

    component_config_schema = TaskAgentConfig
    component_provider_override = "kagent.agents.TaskAgent"

    DEFAULT_DESCRIPTION = "An agent that uses an inner team of agents to generate responses."
    """str: The default description for a TaskAgent."""

    def __init__(
        self,
        name: str,
        team: Team,
        model_context: ChatCompletionContext | None = None,
        *,
        description: str = DEFAULT_DESCRIPTION,
    ) -> None:
        super().__init__(name=name, description=description)
        self._team = team
        self._model_context = model_context or UnboundedChatCompletionContext()

    @property
    def produced_message_types(self) -> Sequence[type[ChatMessage]]:
        return (TextMessage,)

    async def on_messages(self, messages: Sequence[ChatMessage], cancellation_token: CancellationToken) -> Response:
        # Call the stream method and collect the messages.
        response: Response | None = None
        async for msg in self.on_messages_stream(messages, cancellation_token):
            if isinstance(msg, Response):
                response = msg
        assert response is not None
        return response

    async def on_messages_stream(
        self, messages: Sequence[ChatMessage], cancellation_token: CancellationToken
    ) -> AsyncGenerator[AgentEvent | ChatMessage | Response, None]:
        # Run the team of agents.
        result: TaskResult | None = None
        inner_messages: List[AgentEvent | ChatMessage] = []
        count = 0
        context = await self._model_context.get_messages()
        task = list(messages)
        if len(context) > 0:
            message = HandoffMessage(
                content="Here are the relevant previous messages.",
                source=self.name,
                target="",
                context=context,
            )
            task = [message] + list(messages)

        async for inner_msg in self._team.run_stream(task=task, cancellation_token=cancellation_token):
            if isinstance(inner_msg, TaskResult):
                result = inner_msg
            else:
                count += 1
                if count <= len(task):
                    # Skip the task messages.
                    continue
                yield inner_msg
                if isinstance(inner_msg, ModelClientStreamingChunkEvent):
                    # Skip the model client streaming chunk events.
                    continue

                inner_messages.append(inner_msg)
        assert result is not None

        text_result: TextMessage | None = None
        for message in inner_messages:
            if isinstance(message, TextMessage):
                text_result = message
                await self._model_context.add_message(AssistantMessage(content=message.content, source=message.source))
            elif isinstance(message, ToolCallSummaryMessage):
                await self._model_context.add_message(AssistantMessage(content=message.content, source=message.source))
            elif isinstance(message, ToolCallExecutionEvent):
                await self._model_context.add_message(FunctionExecutionResultMessage(content=message.content))
            elif isinstance(message, ToolCallRequestEvent):
                await self._model_context.add_message(AssistantMessage(content=message.content, source=message.source))

        assert text_result is not None
        # Yield the final agent response.
        yield Response(chat_message=text_result, inner_messages=inner_messages)

        # Reset the team.
        await self._team.reset()

    async def on_reset(self, cancellation_token: CancellationToken) -> None:
        await self._team.reset()

    async def save_state(self) -> Mapping[str, Any]:
        team_state = await self._team.save_state()
        model_context_state = await self._model_context.save_state()
        state = TaskAgentState(inner_team_state=team_state, model_context_state=model_context_state)
        return state.model_dump()

    async def load_state(self, state: Mapping[str, Any]) -> None:
        task_agent_state = TaskAgentState.model_validate(state)
        await self._model_context.load_state(task_agent_state.model_context_state)
        await self._team.load_state(task_agent_state.inner_team_state)

    def _to_config(self) -> TaskAgentConfig:
        return TaskAgentConfig(
            name=self.name,
            team=self._team.dump_component(),
            model_context=self._model_context.dump_component(),
            description=self.description,
        )

    @classmethod
    def _from_config(cls, config: TaskAgentConfig) -> Self:
        model_context = (
            ChatCompletionContext.load_component(config.model_context)
            if config.model_context is not None
            else UnboundedChatCompletionContext()
        )
        team = Team.load_component(config.team)
        return cls(
            name=config.name,
            team=team,
            model_context=model_context,
            description=config.description or cls.DEFAULT_DESCRIPTION,
        )
