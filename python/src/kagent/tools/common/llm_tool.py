import logging
import os
from typing import Optional

from autogen_core import CancellationToken, Component
from autogen_core.models import SystemMessage, UserMessage
from autogen_core.tools import BaseTool
from autogen_ext.models.openai import OpenAIChatCompletionClient
from pydantic import BaseModel, Field

logger = logging.getLogger(__name__)


class LLMToolConfig(BaseModel):
    """Configuration for the LLMTool."""

    model: str = Field(default="gpt-4o", description="The model to use for the LLM.")
    openai_api_key: Optional[str] = Field(
        None, description="OpenAI API key. If not provided, will look for it in environment variables."
    )
    temperature: float = Field(0.0, description="Temperature for the model's output.")


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
        self.config = config
        self._model_client = self._create_model_client()

        super().__init__(
            args_type=LLMToolInput,
            return_type=str,
            name="llm_call",
            description="Call an LLM with custom system and user prompts.",
        )

    def _create_model_client(self) -> OpenAIChatCompletionClient:
        """
        Create an OpenAI model client with the configured settings.

        Returns:
            OpenAIChatCompletionClient: The OpenAI model client.
        """
        api_key = self.config.openai_api_key or os.environ.get("OPENAI_API_KEY") or ""
        return OpenAIChatCompletionClient(
            model=self.config.model,
            api_key=api_key,
            temperature=self.config.temperature,
        )

    async def _call_llm(
        self,
        system_prompt: str,
        user_message: str,
        json_output: bool = False,
        cancellation_token: CancellationToken = None,
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

            logger.debug(f"Calling LLM with model: {self.config.model}")
            result = await self._model_client.create(
                messages=[SystemMessage(content=system_prompt), UserMessage(content=user_message, source="user")],
                json_output=json_output,
            )
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
            return await self._call_llm(args.system_prompt, args.user_message, args.json_output, cancellation_token)
        except LLMCallError as e:
            return f"Error: {str(e)}"
        except Exception as e:
            logger.exception(f"Unexpected error in LLMTool: {str(e)}")
            return f"Unexpected error: {str(e)}"

    def _to_config(self) -> LLMToolConfig:
        return self.config

    @classmethod
    def _from_config(cls, config: LLMToolConfig) -> "LLMTool":
        return cls(config)
