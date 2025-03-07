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
    ChatCompletionClient,
    CreateResult,
    FunctionExecutionResultMessage,
    LLMMessage,
    SystemMessage,
    UserMessage,
)
from pydantic import BaseModel, Field
from typing_extensions import Self


class SocietyOfMindAgentState(BaseState):
    """State for a Society of Mind agent."""

    inner_team_state: Mapping[str, Any] = Field(default_factory=dict)
    model_context_state: Mapping[str, Any] = Field(default_factory=dict)
    type: str = Field(default="SocietyOfMindAgentState")


class SocietyOfMindAgentConfig(BaseModel):
    """The declarative configuration for a SocietyOfMindAgent."""

    name: str
    team: ComponentModel
    model_client: ComponentModel
    model_context: ComponentModel
    description: str | None = None
    instruction: str | None = None
    response_prompt: str | None = None
    model_client_stream: bool = False


class SocietyOfMindAgent(BaseChatAgent, Component[SocietyOfMindAgentConfig]):
    """An agent that uses an inner team of agents to generate responses.

    Each time the agent's :meth:`on_messages` or :meth:`on_messages_stream`
    method is called, it runs the inner team of agents and then uses the
    model client to generate a response based on the inner team's messages.
    Once the response is generated, the agent resets the inner team by
    calling :meth:`Team.reset`.

    Args:
        name (str): The name of the agent.
        team (Team): The team of agents to use.
        model_client (ChatCompletionClient): The model client to use for preparing responses.
        description (str, optional): The description of the agent.
        instruction (str, optional): The instruction to use when generating a response using the inner team's messages.
            Defaults to :attr:`DEFAULT_INSTRUCTION`. It assumes the role of 'system'.
        response_prompt (str, optional): The response prompt to use when generating a response using the inner team's messages.
            Defaults to :attr:`DEFAULT_RESPONSE_PROMPT`. It assumes the role of 'system'.


    Example:

    .. code-block:: python

        import asyncio
        from autogen_agentchat.ui import Console
        from autogen_agentchat.agents import AssistantAgent, SocietyOfMindAgent
        from autogen_ext.models.openai import OpenAIChatCompletionClient
        from autogen_agentchat.teams import RoundRobinGroupChat
        from autogen_agentchat.conditions import TextMentionTermination


        async def main() -> None:
            model_client = OpenAIChatCompletionClient(model="gpt-4o")

            agent1 = AssistantAgent(
                "assistant1", model_client=model_client, system_message="You are a writer, write well."
            )
            agent2 = AssistantAgent(
                "assistant2",
                model_client=model_client,
                system_message="You are an editor, provide critical feedback. Respond with 'APPROVE' if the text addresses all feedbacks.",
            )
            inner_termination = TextMentionTermination("APPROVE")
            inner_team = RoundRobinGroupChat([agent1, agent2], termination_condition=inner_termination)

            society_of_mind_agent = SocietyOfMindAgent("society_of_mind", team=inner_team, model_client=model_client)

            agent3 = AssistantAgent(
                "assistant3", model_client=model_client, system_message="Translate the text to Spanish."
            )
            team = RoundRobinGroupChat([society_of_mind_agent, agent3], max_turns=2)

            stream = team.run_stream(task="Write a short story with a surprising ending.")
            await Console(stream)


        asyncio.run(main())
    """

    component_config_schema = SocietyOfMindAgentConfig
    component_provider_override = "kagent.agents.SocietyOfMindAgent"

    DEFAULT_INSTRUCTION = "Earlier you were asked to fulfill a request. You and your team worked diligently to address that request. Here is a transcript of that conversation:"
    """str: The default instruction to use when generating a response using the
    inner team's messages. The instruction will be prepended to the inner team's
    messages when generating a response using the model. It assumes the role of
    'system'."""

    DEFAULT_RESPONSE_PROMPT = "Output a standalone response to the original request, summarizing the tools used to address the request, as well as highlighting important parts of the result"
    """str: The default response prompt to use when generating a response using
    the inner team's messages. It assumes the role of 'system'."""

    DEFAULT_DESCRIPTION = "An agent that uses an inner team of agents to generate responses."
    """str: The default description for a SocietyOfMindAgent."""

    def __init__(
        self,
        name: str,
        team: Team,
        model_client: ChatCompletionClient,
        model_context: ChatCompletionContext | None = None,
        *,
        description: str = DEFAULT_DESCRIPTION,
        instruction: str = DEFAULT_INSTRUCTION,
        response_prompt: str = DEFAULT_RESPONSE_PROMPT,
        model_client_stream: bool = False,
    ) -> None:
        super().__init__(name=name, description=description)
        self._team = team
        self._model_client = model_client
        self._model_context = model_context or UnboundedChatCompletionContext()
        self._instruction = instruction
        self._response_prompt = response_prompt
        self._model_client_stream = model_client_stream

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

        if len(inner_messages) == 0:
            yield Response(
                chat_message=TextMessage(source=self.name, content="No response."), inner_messages=inner_messages
            )
        else:
            # Generate a response using the model client.
            llm_messages: List[LLMMessage] = [SystemMessage(content=self._instruction)]
            # Add the messages to the context, as well as create context for the summary of the tool calls.
            for message in inner_messages:
                if isinstance(message, TextMessage):
                    await self._model_context.add_message(
                        AssistantMessage(content=message.content, source=message.source)
                    )
                    llm_messages.append(UserMessage(content=message.content, source=message.source))
                elif isinstance(message, ToolCallSummaryMessage):
                    await self._model_context.add_message(
                        AssistantMessage(content=message.content, source=message.source)
                    )
                    llm_messages.append(UserMessage(content=message.content, source=message.source))
                elif isinstance(message, ToolCallExecutionEvent):
                    await self._model_context.add_message(FunctionExecutionResultMessage(content=message.content))
                elif isinstance(message, ToolCallRequestEvent):
                    await self._model_context.add_message(
                        AssistantMessage(content=message.content, source=message.source)
                    )
            ## TODO: Explain to the model that the inner are in order summaries of tool calls and their outputs.
            llm_messages.append(SystemMessage(content=self._response_prompt))
            if self._model_client_stream:
                model_result: CreateResult | None = None
                # Stream the model client.
                async for chunk in self._model_client.create_stream(
                    llm_messages,
                    cancellation_token=cancellation_token,
                    # json_output=self._response_json_type is not None,
                    # extra_create_args={"response_format": self._response_json_type},
                ):
                    if isinstance(chunk, CreateResult):
                        model_result = chunk
                    elif isinstance(chunk, str):
                        yield ModelClientStreamingChunkEvent(content=chunk, source=self.name)
                    else:
                        raise RuntimeError(f"Invalid chunk type: {type(chunk)}")
                assert isinstance(model_result, CreateResult)
                assert isinstance(model_result.content, str)
                await self._model_context.add_message(AssistantMessage(content=model_result.content, source=self.name))
                yield Response(
                    chat_message=TextMessage(
                        source=self.name, content=model_result.content, models_usage=model_result.usage
                    ),
                    inner_messages=inner_messages,
                )
            else:
                completion = await self._model_client.create(
                    messages=llm_messages,
                    cancellation_token=cancellation_token,
                    # json_output=self._response_json_type is not None,
                    # extra_create_args={"response_format": self._response_json_type},
                )
                assert isinstance(completion.content, str)
                await self._model_context.add_message(AssistantMessage(content=completion.content, source=self.name))
                yield Response(
                    chat_message=TextMessage(
                        source=self.name, content=completion.content, models_usage=completion.usage
                    ),
                    inner_messages=inner_messages,
                )

        # Reset the team.
        await self._team.reset()

    async def on_reset(self, cancellation_token: CancellationToken) -> None:
        await self._team.reset()

    async def save_state(self) -> Mapping[str, Any]:
        team_state = await self._team.save_state()
        model_context_state = await self._model_context.save_state()
        state = SocietyOfMindAgentState(inner_team_state=team_state, model_context_state=model_context_state)
        return state.model_dump()

    async def load_state(self, state: Mapping[str, Any]) -> None:
        society_of_mind_state = SocietyOfMindAgentState.model_validate(state)
        await self._model_context.load_state(society_of_mind_state.model_context_state)
        await self._team.load_state(society_of_mind_state.inner_team_state)

    def _to_config(self) -> SocietyOfMindAgentConfig:
        return SocietyOfMindAgentConfig(
            name=self.name,
            team=self._team.dump_component(),
            model_client=self._model_client.dump_component(),
            model_context=self._model_context.dump_component(),
            description=self.description,
            instruction=self._instruction,
            response_prompt=self._response_prompt,
            model_client_stream=self._model_client_stream,
        )

    @classmethod
    def _from_config(cls, config: SocietyOfMindAgentConfig) -> Self:
        model_client = ChatCompletionClient.load_component(config.model_client)
        model_context = ChatCompletionContext.load_component(config.model_context)
        team = Team.load_component(config.team)
        return cls(
            name=config.name,
            team=team,
            model_client=model_client,
            model_context=model_context,
            description=config.description or cls.DEFAULT_DESCRIPTION,
            instruction=config.instruction or cls.DEFAULT_INSTRUCTION,
            response_prompt=config.response_prompt or cls.DEFAULT_RESPONSE_PROMPT,
            model_client_stream=config.model_client_stream,
        )
