from typing import Sequence

from autogen_agentchat.base import TerminatedException, TerminationCondition
from autogen_agentchat.messages import BaseAgentEvent, BaseChatMessage, StopMessage, TextMessage
from autogen_core import Component
from pydantic import BaseModel
from typing_extensions import Self


class FinalTextMessageTerminationConfig(BaseModel):
    """Configuration for the FinalTextMessageTermination termination condition."""

    source: str | None = None
    """The source of the text message to terminate the conversation."""


class FinalTextMessageTermination(TerminationCondition, Component[FinalTextMessageTerminationConfig]):
    """Terminate the conversation if a :class:`~autogen_agentchat.messages.TextMessage` is received in the FINAL_TEXT_MESSAGE message.

    This termination condition checks for TextMessage instances in the message sequence. When a TextMessage is found,
    it terminates the conversation if either:
    - No source was specified (terminates on any TextMessage)
    - The message source matches the specified source

    Args:
        source (str | None, optional): The source name to match against incoming messages. If None, matches any source.
            Defaults to None.
    """

    component_config_schema = FinalTextMessageTerminationConfig
    component_provider_override = "autogen_agentchat.conditions.FinalTextMessageTermination"

    def __init__(self, source: str | None = None) -> None:
        self._terminated = False
        self._source = source

    @property
    def terminated(self) -> bool:
        return self._terminated

    async def __call__(self, messages: Sequence[BaseAgentEvent | BaseChatMessage]) -> StopMessage | None:
        if self._terminated:
            raise TerminatedException("Termination condition has already been reached")
        if len(messages) == 0:
            return None
        final_message = messages[-1]
        if isinstance(final_message, TextMessage) and (self._source is None or final_message.source == self._source):
            self._terminated = True
            return StopMessage(
                content=f"Text message received from '{final_message.source}'", source="FinalTextMessageTermination"
            )
        return None

    async def reset(self) -> None:
        self._terminated = False

    def _to_config(self) -> FinalTextMessageTerminationConfig:
        return FinalTextMessageTerminationConfig(source=self._source)

    @classmethod
    def _from_config(cls, config: FinalTextMessageTerminationConfig) -> Self:
        return cls(source=config.source)
