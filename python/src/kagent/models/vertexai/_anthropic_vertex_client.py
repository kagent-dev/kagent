import inspect

from anthropic import AsyncAnthropicVertex
from autogen_core import Component
from autogen_ext.models.anthropic import BaseAnthropicChatCompletionClient
from google.auth import load_credentials_from_dict
from typing_extensions import Any, Dict, Mapping, Optional, Self, Set, Unpack

from ._model_info import ModelInfo, get_info
from .config import AnthropicVertexAIClientConfiguration

# Common parameters for message creation
anthropic_message_params = {
    "system",
    "messages",
    "max_tokens",
    "temperature",
    "top_p",
    "top_k",
    "stop_sequences",
    "tools",
    "tool_choice",
    "stream",
    "metadata",
}
disallowed_create_args = {"stream", "messages"}
required_create_args: Set[str] = {"model"}

anthropic_init_kwargs = set(inspect.getfullargspec(AsyncAnthropicVertex.__init__).kwonlyargs)


def _create_args_from_config(config: Mapping[str, Any]) -> Dict[str, Any]:
    create_args = {k: v for k, v in config.items() if k in anthropic_message_params or k == "model"}
    create_args_keys = set(create_args.keys())

    if not required_create_args.issubset(create_args_keys):
        raise ValueError(f"Required create args are missing: {required_create_args - create_args_keys}")

    if disallowed_create_args.intersection(create_args_keys):
        raise ValueError(f"Disallowed create args are present: {disallowed_create_args.intersection(create_args_keys)}")

    return create_args


def _anthropic_client_from_config(config: Mapping[str, Any]) -> AsyncAnthropicVertex:
    # Filter config to only include valid parameters
    client_config = {k: v for k, v in config.items() if k in anthropic_init_kwargs}
    return AsyncAnthropicVertex(**client_config)


class AnthropicVertexAIChatCompletionClient(
    BaseAnthropicChatCompletionClient, Component[AnthropicVertexAIClientConfiguration]
):
    component_type = "model"
    component_config_schema = AnthropicVertexAIClientConfiguration
    component_provider_override = "kagent.models.vertexai.AnthropicVertexAIChatCompletionClient"

    def __init__(self, **kwargs: Unpack[AnthropicVertexAIClientConfiguration]):
        if "model" not in kwargs:
            raise ValueError("model is required for AnthropicVertexAIChatCompletionClient")

        self._raw_config: Dict[str, Any] = dict(kwargs).copy()
        copied_args = dict(kwargs).copy()

        model_info: Optional[ModelInfo] = None
        if "model_info" in kwargs:
            model_info = kwargs["model_info"]
            del copied_args["model_info"]

        if "model" in kwargs:
            model_info = get_info(kwargs["model"])

        if not model_info:
            raise ValueError("model_info or model is required for AnthropicVertexAIChatCompletionClient")

        if "credentials" in kwargs:
            credentials = kwargs["credentials"]
            del copied_args["credentials"]
        else:
            raise ValueError("credentials is required for AnthropicVertexAIChatCompletionClient")

        if "project" in kwargs:
            project = kwargs["project"]
            del copied_args["project"]
        else:
            raise ValueError("project is required for AnthropicVertexAIChatCompletionClient")

        if "location" in kwargs:
            location = kwargs["location"]
            del copied_args["location"]
        else:
            raise ValueError("location is required for AnthropicVertexAIChatCompletionClient")

        # need to explicitly provide the scopes for the credentials, otherwise it will not work
        google_creds = load_credentials_from_dict(
            credentials, scopes=["https://www.googleapis.com/auth/cloud-platform"]
        )

        client = AsyncAnthropicVertex(
            region=location,
            project_id=project,
            credentials=google_creds[0],
        )
        create_args = _create_args_from_config(copied_args)

        super().__init__(
            client=client,
            create_args=create_args,
            model_info=model_info,
        )

    def __getstate__(self) -> Dict[str, Any]:
        state = self.__dict__.copy()
        state["_client"] = None
        return state

    def __setstate__(self, state: Dict[str, Any]) -> None:
        self.__dict__.update(state)
        self._client = _anthropic_client_from_config(state["_raw_config"])

    def _to_config(self) -> AnthropicVertexAIClientConfiguration:
        copied_config = self._raw_config.copy()
        return AnthropicVertexAIClientConfiguration(**copied_config)

    @classmethod
    def _from_config(cls, config: AnthropicVertexAIClientConfiguration) -> Self:
        copied_config = config.model_copy().model_dump(exclude_none=True)
        return cls(**copied_config)
