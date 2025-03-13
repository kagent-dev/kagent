import logging
from typing import Optional

from autogen_core import CancellationToken, Component
from autogen_core.tools import BaseTool
from pydantic import BaseModel, Field

from ..common import LLMCallError, LLMTool, LLMToolConfig, LLMToolInput
from ._prompt_registry import get_system_prompt
from ._resource_types import ResourceTypes

logger = logging.getLogger(__name__)


class GenerateResourceToolConfig(LLMToolConfig):
    """Configuration for the GenerateResourceTool."""

    pass


class GenerateResourceToolInput(BaseModel):
    """Input for the GenerateResourceTool."""

    resource_description: str = Field(..., description="The description of the resource to generate.")
    resource_type: ResourceTypes = Field(..., description="The type of resource to generate.")


class GenerateResourceError(LLMCallError):
    """Exception raised for errors in the resource generation process."""

    pass


class GenerateResourceTool(BaseTool, Component[GenerateResourceToolConfig]):
    """
    GenerateResourceTool that generates YAML configuration from a detailed description,
    built on top of the GenericLLMTool.

    Args:
        config (GenerateResourceToolConfig): Configuration for the GenerateResourceTool.
    """

    component_description = "GenerateResourceTool knows how to generate a resource YAML configuration for Istio, Gateway API, Argo resources from a detailed description."
    component_type = "tool"
    component_config_schema = GenerateResourceToolConfig
    component_provider_override = "kagent.tools.k8s.GenerateResourceTool"

    def __init__(self, config: GenerateResourceToolConfig) -> None:
        self.config = config
        self._llm_tool = LLMTool(config)

        super().__init__(
            args_type=GenerateResourceToolInput,
            return_type=str,
            name="GenerateResourceTool",
            description="Generates a resource YAML configuration from a detailed description.",
        )

    async def _generate_resource(
        self,
        policy_description: str,
        resource_type: ResourceTypes,
        cancellation_token: CancellationToken,
    ) -> str:
        """
        Asynchronously generates a resource YAML based on the
        provided system prompt and policy description.

        Args:
            policy_description: The description of the policy to be included in the resource YAML.
            resource_type: The type of resource to generate.
            cancellation_token: Token to signal cancellation.

        Returns:
            str: The generated YAML or an error message if the generation fails.

        Raises:
            GenerateResourceError: If there is an error during the resource generation process.
        """
        try:
            if cancellation_token and cancellation_token.is_cancelled():
                raise Exception("Operation cancelled")

            # Get the system prompt for the specific resource type
            system_prompt = get_system_prompt(resource_type)
            logger.debug(f"Generating resource for type '{resource_type}'")

            result = await self._llm_tool.run(
                LLMToolInput(system_prompt=system_prompt, user_message=policy_description, json_output=True),
                cancellation_token,
            )
            return result
        except ValueError as e:
            logger.error(f"Invalid resource type: {str(e)}")
            raise GenerateResourceError(f"Invalid resource type: {str(e)}") from e
        except Exception as e:
            logger.error(f"Error generating resource: {str(e)}")
            raise GenerateResourceError(f"Error generating resource: {str(e)}") from e

    async def run(self, args: GenerateResourceToolInput, cancellation_token: CancellationToken) -> str:
        """
        Generate resource YAML configuration from a detailed description.

        Args:
            args: The arguments to pass to the tool.
            cancellation_token: Token to signal cancellation.

        Returns:
            The generated YAML as a string.
        """
        try:
            return await self._generate_resource(args.resource_description, args.resource_type, cancellation_token)
        except GenerateResourceError as e:
            return f"Error: {str(e)}"
        except Exception as e:
            logger.exception(f"Unexpected error in GenerateResourceTool: {str(e)}")
            return f"Unexpected error: {str(e)}"

    def _to_config(self) -> GenerateResourceToolConfig:
        return self.config

    @classmethod
    def _from_config(cls, config: GenerateResourceToolConfig) -> "GenerateResourceTool":
        return cls(config)
