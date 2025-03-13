import logging

from autogen_core import CancellationToken, Component, ComponentModel
from autogen_core.models import ChatCompletionClient, SystemMessage, UserMessage
from autogen_core.tools import BaseTool
from pydantic import BaseModel, Field

logger = logging.getLogger(__name__)


class LLMToolConfig(BaseModel):
    """Configuration for the LLMTool."""

    model_client: ComponentModel


class LLMToolInput(BaseModel):
    """Input for the LLMTool."""

    system_prompt: str = Field(..., description="The system prompt to send to the LLM.")
    user_message: str = Field(..., description="The user message to send to the LLM.")
    json_output: bool = Field(False, description="Whether to request JSON output from the LLM.")


class LLMCallError(Exception):
    """Exception raised for errors in the LLM calling process."""

    pass


class LLMTool(BaseTool, Component[LLMToolConfig]):
    """
    LLMTool that provides a generic interface to call an LLM with system and user messages.

    Args:
        config (LLMToolConfig): Configuration for the LLMTool.
    """

    component_description = (
        "LLMTool provides a flexible interface to call an LLM with customizable prompts and settings."
    )
    component_type = "tool"
    component_config_schema = LLMToolConfig
    component_provider_override = "kagent.tools.common.LLMTool"

    def __init__(self, config: LLMToolConfig) -> None:
        self._model_client = ChatCompletionClient.load_component(config.model_client)

        super().__init__(
            args_type=LLMToolInput,
            return_type=str,
            name="llm_call",
            description="Call an LLM with custom system and user prompts.",
        )

    async def _call_llm(
        self,
        system_prompt: str,
        user_message: str,
        cancellation_token: CancellationToken,
        json_output: bool = False,
    ) -> str:
        """
        Asynchronously calls the LLM with the provided system prompt and user message.

        Args:
            system_prompt: The system prompt to send to the LLM.
            user_message: The user message to send to the LLM.
            json_output: Whether to request JSON output from the LLM.
            cancellation_token: Token to signal cancellation.

        Returns:
            str: The LLM's response content.

        Raises:
            LLMCallError: If there is an error during the LLM call.
        """
        try:
            if cancellation_token and cancellation_token.is_cancelled():
                raise Exception("Operation cancelled")

            result = await self._model_client.create(
                messages=[SystemMessage(content=system_prompt), UserMessage(content=user_message, source="user")],
                json_output=json_output,
            )
            # This should always be a string since we're not passing tools, but we'll assert it to be safe
            assert isinstance(result.content, str)
            return result.content
        except Exception as e:
            logger.error(f"Error calling LLM: {str(e)}")
            raise LLMCallError(f"Error calling LLM: {str(e)}") from e

    async def run(self, args: LLMToolInput, cancellation_token: CancellationToken) -> str:
        """
        Call the LLM with the provided inputs.

        Args:
            args: The arguments to pass to the tool.
            cancellation_token: Token to signal cancellation.

        Returns:
            The LLM's response as a string.
        """
        try:
            return await self._call_llm(args.system_prompt, args.user_message, cancellation_token, args.json_output)
        except LLMCallError as e:
            return f"Error: {str(e)}"
        except Exception as e:
            logger.exception(f"Unexpected error in LLMTool: {str(e)}")
            return f"Unexpected error: {str(e)}"

    def _to_config(self) -> LLMToolConfig:
        return LLMToolConfig(model_client=self._model_client.dump_component())

    @classmethod
    def _from_config(cls, config: LLMToolConfig) -> "LLMTool":
        return cls(config)
