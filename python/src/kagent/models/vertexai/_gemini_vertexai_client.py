import asyncio
import base64
import json
import logging
import re
import warnings
from typing import (
    Any,
    AsyncGenerator,
    Dict,
    List,
    Mapping,
    Optional,
    Sequence,
    Union,
    Unpack,
)

from autogen_core import (
    EVENT_LOGGER_NAME,
    TRACE_LOGGER_NAME,
    CancellationToken,
    Component,
    FunctionCall,
)
from autogen_core.logging import LLMCallEvent, LLMStreamEndEvent, LLMStreamStartEvent  # type: ignore
from autogen_core.models import (
    AssistantMessage,
    ChatCompletionClient,
    CreateResult,
    FinishReasons,
    FunctionExecutionResultMessage,
    LLMMessage,
    ModelCapabilities,  # type: ignore
    ModelInfo,
    RequestUsage,
    SystemMessage,
    UserMessage,
    validate_model_info,
)
from autogen_core.tools import Tool, ToolSchema
from google import genai
from google.auth import load_credentials_from_dict
from google.genai import types as genai_types
from google.genai.types import Content, GenerationConfig, Part
from pydantic import BaseModel, SecretStr

from ._model_info import get_info, get_token_limit
from .config import GeminiVertexAIClientConfiguration


# Name validation for Gemini tools
def normalize_gemini_name(name: str) -> str:
    """Normalize names by replacing invalid characters with underscore for Gemini tools."""
    return re.sub(r"[^a-zA-Z0-9_]", "_", name)[:63]  # Gemini limit seems to be 63 chars


def assert_valid_gemini_name(name: str) -> str:
    """Ensure that configured names are valid for Gemini, raises ValueError if not."""
    if not re.match(r"^[a-zA-Z0-9_]{1,63}$", name):
        raise ValueError(
            f"Invalid Gemini tool/function name: {name}. Must be 1-63 chars, letters, numbers, or underscores."
        )
    return name


logger = logging.getLogger(EVENT_LOGGER_NAME)
trace_logger = logging.getLogger(TRACE_LOGGER_NAME)


def _add_usage(usage1: RequestUsage, usage2: RequestUsage) -> RequestUsage:
    return RequestUsage(
        prompt_tokens=(usage1.prompt_tokens or 0) + (usage2.prompt_tokens or 0),
        completion_tokens=(usage1.completion_tokens or 0) + (usage2.completion_tokens or 0),
    )


def _normalize_gemini_finish_reason(reason: Optional[genai_types.FinishReason]) -> FinishReasons:
    if reason is None:
        return "unknown"
    mapping = {
        genai_types.FinishReason.FINISH_REASON_UNSPECIFIED: "unknown",
        genai_types.FinishReason.STOP: "stop",
        genai_types.FinishReason.MAX_TOKENS: "length",
        genai_types.FinishReason.SAFETY: "content_filter",
        genai_types.FinishReason.RECITATION: "content_filter",  # Similar to content filter
        genai_types.FinishReason.OTHER: "unknown",
    }
    return mapping.get(reason, "unknown")


class GeminiVertexAIChatCompletionClient(ChatCompletionClient, Component[GeminiVertexAIClientConfiguration]):
    component_type = "model"
    component_config_schema = GeminiVertexAIClientConfiguration
    component_provider_override = "kagent.models.vertexai.GeminiVertexAIChatCompletionClient"

    def __init__(self, **kwargs: Unpack[GeminiVertexAIClientConfiguration]):
        resolved_config = GeminiVertexAIClientConfiguration(**kwargs)

        self._model_name = resolved_config.model
        self._raw_config: Dict[str, Any] = resolved_config.model_dump(warnings=False)

        client_options_dict: Dict[str, Any] = {}
        if not resolved_config.project or not resolved_config.location or not resolved_config.credentials:
            raise ValueError("project, location, and credentials are required for Vertex AI.")

        # need to explicitly provide the scopes for the credentials, otherwise it will not work
        google_creds = load_credentials_from_dict(
            resolved_config.credentials, scopes=["https://www.googleapis.com/auth/cloud-platform"]
        )
        client_options_dict["credentials"] = google_creds[0]
        client_options_dict["project"] = resolved_config.project
        client_options_dict["location"] = resolved_config.location
        client_options_dict["vertexai"] = True

        self._client = genai.Client(**client_options_dict)

        if resolved_config.model_info_override:
            self._model_info = resolved_config.model_info_override
        else:
            self._model_info = get_info(self._model_name)
        validate_model_info(self._model_info)

        self._create_args = GenerationConfig(
            **{
                k: v
                for k, v in {
                    "temperature": resolved_config.temperature,
                    "top_p": resolved_config.top_p,
                    "top_k": resolved_config.top_k,
                    "max_output_tokens": resolved_config.max_output_tokens,
                    "stop_sequences": resolved_config.stop_sequences,
                }.items()
                if v is not None
            }
        )

        self._total_usage = RequestUsage(prompt_tokens=0, completion_tokens=0)
        self._actual_usage = RequestUsage(prompt_tokens=0, completion_tokens=0)
        self._last_used_tools: Optional[List[genai_types.Tool]] = None

    def _convert_message_to_gemini(self, message: LLMMessage) -> Optional[Content | List[Content]]:
        """Converts a single LLMMessage to Gemini Content or list of Contents."""
        parts: List[Part] = []
        role: str = "user"

        if isinstance(message, UserMessage):
            role = "user"
            if isinstance(message.content, str):
                parts.append(Part(text=message.content if message.content.strip() else " "))
            elif isinstance(message.content, list):
                for item in message.content:
                    if isinstance(item, str):
                        parts.append(Part(text=item if item.strip() else " "))
                    # TODO: Add support for images
                    else:
                        logger.warning(f"Unsupported content type in UserMessage: {type(item)}")
            return Content(parts=parts, role=role)

        elif isinstance(message, AssistantMessage):
            role = "model"
            if isinstance(message.content, str):
                parts.append(Part(text=message.content))
            elif isinstance(message.content, list):
                for func_call in message.content:
                    if isinstance(func_call, FunctionCall):
                        args = func_call.arguments
                        try:
                            args_dict = json.loads(args) if isinstance(args, str) else args
                        except json.JSONDecodeError:
                            args_dict = {"_raw_arguments": args}
                            logger.warning(
                                f"Function call arguments for {func_call.name} are not valid JSON. Passing as raw string."
                            )

                        parts.append(
                            Part(
                                function_call=genai_types.FunctionCall(
                                    name=normalize_gemini_name(func_call.name), args=args_dict
                                )
                            )
                        )
                    else:
                        logger.warning(f"Unsupported content type in AssistantMessage list: {type(func_call)}")
            return Content(parts=parts, role=role)

        elif isinstance(message, FunctionExecutionResultMessage):
            gemini_parts: List[Part] = []
            for result in message.content:
                try:
                    content_value = json.loads(result.content) if isinstance(result.content, str) else result.content
                except json.JSONDecodeError:
                    content_value = {"_raw_content": str(result.content)}

                gemini_parts.append(
                    Part(
                        function_response=genai_types.FunctionResponse(
                            name=normalize_gemini_name(result.name),  # Name of the function that was called
                            response={"content": content_value},  # Gemini expects a dict, 'content' is a common key
                        )
                    )
                )
            return Content(parts=gemini_parts, role="user")

        elif isinstance(message, SystemMessage):
            return None

    def _convert_tools_to_gemini(self, tools: Sequence[Tool | ToolSchema]) -> Optional[List[genai_types.Tool]]:
        if not tools:
            return None

        gemini_tools: List[genai_types.FunctionDeclaration] = []
        for tool_spec in tools:
            schema: ToolSchema
            if isinstance(tool_spec, Tool):
                schema = tool_spec.schema
            else:  # It's a dict (ToolSchema)
                schema = tool_spec

            assert_valid_gemini_name(schema["name"])

            parameters_schema: Optional[genai_types.Schema] = None
            if "parameters" in schema and schema["parameters"]:
                raw_params = schema["parameters"]

                def to_gemini_schema(json_schema_props: Dict[str, Any]) -> genai_types.Schema:
                    type_mapping = {
                        "string": genai_types.Type.STRING,
                        "number": genai_types.Type.NUMBER,  # float/double
                        "integer": genai_types.Type.INTEGER,
                        "boolean": genai_types.Type.BOOLEAN,
                        "object": genai_types.Type.OBJECT,
                        "array": genai_types.Type.ARRAY,
                    }

                    gemini_type = type_mapping.get(json_schema_props.get("type", "object").lower())
                    if gemini_type is None:
                        logger.warning(
                            f"Unsupported schema type: {json_schema_props.get('type')}. Defaulting to STRING."
                        )
                        gemini_type = genai_types.Type.STRING

                    props = None
                    if "properties" in json_schema_props and json_schema_props["properties"]:
                        props = {k: to_gemini_schema(v) for k, v in json_schema_props["properties"].items()}

                    items_schema = None
                    if (
                        "items" in json_schema_props
                        and json_schema_props["items"]
                        and gemini_type == genai_types.Type.ARRAY
                    ):
                        items_schema = to_gemini_schema(json_schema_props["items"])

                    return genai_types.Schema(
                        type=gemini_type,
                        description=json_schema_props.get("description", ""),
                        properties=props,
                        required=json_schema_props.get("required", None),
                        items=items_schema,
                        enum=json_schema_props.get("enum", None),
                    )

                if raw_params.get("type") == "object" and "properties" in raw_params:
                    parameters_schema = to_gemini_schema(raw_params)
                else:
                    logger.warning(
                        f"Tool parameters for {schema['name']} are not a simple object schema, might not be fully compatible."
                    )
                    parameters_schema = genai_types.Schema(type=genai_types.Type.OBJECT)

            gemini_tools.append(
                genai_types.FunctionDeclaration(
                    name=normalize_gemini_name(schema["name"]),
                    description=schema.get("description", ""),
                    parameters=parameters_schema,
                )
            )
        return [genai_types.Tool(function_declarations=gemini_tools)] if gemini_tools else None

    async def create(
        self,
        messages: Sequence[LLMMessage],
        *,
        tools: Sequence[Tool | ToolSchema] = [],
        json_output: Optional[bool | type[BaseModel]] = None,
        extra_create_args: Mapping[str, Any] = {},
        cancellation_token: Optional[CancellationToken] = None,
    ) -> CreateResult:
        if self._model_info.get("function_calling", False) is False and len(tools) > 0:
            raise ValueError("Model does not support function calling/tools, but tools were provided.")

        final_create_args = self._create_args.model_copy()
        allowed_extra_keys = {
            "temperature",
            "top_p",
            "top_k",
            "max_output_tokens",
            "stop_sequences",
            "response_mime_type",
            "candidate_count",
        }
        for k, v in extra_create_args.items():
            if k in allowed_extra_keys:
                setattr(final_create_args, k, v)
            else:
                logger.warning(f"Unsupported extra_create_arg: {k}")

        if json_output:
            if self._model_info.get("json_output", False) is False and json_output is True:
                logger.warning(
                    "Model's declared json_output capability is False, but JSON output was requested. Attempting anyway."
                )
            if json_output is True:
                final_create_args.response_mime_type = "application/json"
            elif isinstance(json_output, type) and issubclass(json_output, BaseModel):
                logger.warning(
                    "Pydantic model-based JSON output is not yet fully implemented for Gemini. Use json_output=True for generic JSON."
                )
                final_create_args.response_mime_type = "application/json"

        system_instruction_content: Optional[str] = None
        gemini_contents: List[Content] = []
        merged_system_message_str = ""
        regular_messages: List[LLMMessage] = []
        first_system_idx = -1

        for idx, msg in enumerate(messages):
            if isinstance(msg, SystemMessage):
                merged_system_message_str += (msg.content if msg.content.strip() else " ") + "\n"
                if first_system_idx == -1:
                    first_system_idx = idx
            else:
                regular_messages.append(msg)

        if merged_system_message_str:
            system_instruction_content = merged_system_message_str.strip()

        for i, autogen_msg in enumerate(regular_messages):
            converted_msg_obj = self._convert_message_to_gemini(autogen_msg)
            if isinstance(converted_msg_obj, Content):
                if converted_msg_obj.role not in ["user", "model"]:
                    logger.warning(
                        f"Message role '{converted_msg_obj.role}' not 'user' or 'model'. Adjusting to 'user'."
                    )
                    converted_msg_obj.role = "user"

                if (
                    i == len(regular_messages) - 1
                    and converted_msg_obj.role == "user"
                    and not any(p.text or p.inline_data for p in converted_msg_obj.parts if p)
                ):  # check if parts exist
                    logger.warning("Last message is an empty user message. This might cause issues.")
                gemini_contents.append(converted_msg_obj)
            elif isinstance(converted_msg_obj, list):
                gemini_contents.extend(converted_msg_obj)

        gemini_tools_converted = self._convert_tools_to_gemini(tools)
        self._last_used_tools = gemini_tools_converted

        gen_content_config = genai_types.GenerateContentConfig(
            system_instruction=system_instruction_content if system_instruction_content else None,
            temperature=final_create_args.temperature,
            top_p=final_create_args.top_p,
            top_k=final_create_args.top_k,
            max_output_tokens=final_create_args.max_output_tokens,
            tools=gemini_tools_converted if gemini_tools_converted else None,
        )
        logger.info(
            LLMCallEvent(
                messages=[msg.model_dump_json() for msg in messages],
                response=None,
                prompt_tokens=None,
                completion_tokens=None,
            )
        )

        api_task = asyncio.ensure_future(
            self._client.aio.models.generate_content(
                model=self._model_name, contents=gemini_contents, config=gen_content_config
            )
        )

        if cancellation_token:
            cancellation_token.link_future(api_task)

        try:
            response: genai_types.GenerateContentResponse = await api_task
        except Exception as e:
            logger.error(f"Gemini API call failed: {e}")
            raise

        prompt_tokens_val = response.usage_metadata.prompt_token_count if response.usage_metadata else 0
        completion_tokens_val = response.usage_metadata.candidates_token_count if response.usage_metadata else 0
        usage = RequestUsage(
            prompt_tokens=prompt_tokens_val,
            completion_tokens=completion_tokens_val,
        )
        self._total_usage = _add_usage(self._total_usage, usage)
        self._actual_usage = _add_usage(self._actual_usage, usage)

        logger.info(
            LLMCallEvent(
                messages=None,
                response=response.to_dict(),
                prompt_tokens=usage.prompt_tokens,
                completion_tokens=usage.completion_tokens,
            )
        )

        if not response.candidates:
            prompt_feedback_info = response.prompt_feedback if response.prompt_feedback else "No specific feedback."
            logger.warning(f"Gemini response has no candidates. Prompt feedback: {prompt_feedback_info}")
            finish_reason_from_feedback: FinishReasons = "unknown"
            if response.prompt_feedback and response.prompt_feedback.block_reason:
                finish_reason_from_feedback = "content_filter"

            return CreateResult(
                finish_reason=finish_reason_from_feedback, content="", usage=usage, cached=False, thought=None
            )

        candidate = response.candidates[0]
        finish_reason = _normalize_gemini_finish_reason(candidate.finish_reason)
        final_content: Union[str, List[FunctionCall]]
        thought_content: Optional[str] = None

        function_calls_parts = [part for part in candidate.content.parts if part.function_call]
        if function_calls_parts:
            autogen_fcs: List[FunctionCall] = []
            for part_fc in function_calls_parts:
                fc = part_fc.function_call
                normalized_name = normalize_gemini_name(fc.name)
                autogen_fcs.append(
                    FunctionCall(
                        id=f"call_{normalized_name}_{len(autogen_fcs)}",
                        name=normalized_name,
                        arguments=json.dumps(fc.args) if fc.args else "{}",
                    )
                )
            final_content = autogen_fcs
            text_parts = [part.text for part in candidate.content.parts if hasattr(part, "text") and part.text]
            if text_parts:
                thought_content = "\n".join(text_parts).strip()
        else:
            all_text_parts = [part.text for part in candidate.content.parts if hasattr(part, "text") and part.text]
            final_content = "".join(all_text_parts)
            if final_create_args.response_mime_type == "application/json" and isinstance(final_content, str):
                try:
                    json.loads(final_content)
                except json.JSONDecodeError:
                    logger.warning("JSON output was requested, but the response is not valid JSON.")

        return CreateResult(
            finish_reason=finish_reason, content=final_content, usage=usage, cached=False, thought=thought_content
        )

    async def create_stream(
        self,
        messages: Sequence[LLMMessage],
        *,
        tools: Sequence[Tool | ToolSchema] = [],
        json_output: Optional[bool | type[BaseModel]] = None,
        extra_create_args: Mapping[str, Any] = {},
        cancellation_token: Optional[CancellationToken] = None,
    ) -> AsyncGenerator[Union[str, CreateResult], None]:
        if self._model_info.get("function_calling", False) is False and len(tools) > 0:
            raise ValueError("Model does not support function calling/tools, but tools were provided.")

        final_create_args = self._create_args.model_copy()
        allowed_extra_keys = {
            "temperature",
            "top_p",
            "top_k",
            "max_output_tokens",
            "stop_sequences",
            "response_mime_type",
            "candidate_count",
        }
        for k, v in extra_create_args.items():
            if k in allowed_extra_keys:
                setattr(final_create_args, k, v)
            else:
                logger.warning(f"Unsupported extra_create_arg for stream: {k}")

        if json_output:
            if self._model_info.get("json_output", False) is False and json_output is True:
                logger.warning(
                    "Model's declared json_output capability is False, but JSON output was requested for stream. Attempting anyway."
                )
            if json_output is True:
                final_create_args.response_mime_type = "application/json"
            elif isinstance(json_output, type) and issubclass(json_output, BaseModel):
                logger.warning(
                    "Pydantic model-based JSON output is not yet fully implemented for Gemini stream. Use json_output=True."
                )
                final_create_args.response_mime_type = "application/json"

        system_instruction_content: Optional[str] = None
        gemini_contents: List[Content] = []
        merged_system_message_str = ""
        regular_messages: List[LLMMessage] = []
        first_system_idx = -1

        for idx, msg in enumerate(messages):
            if isinstance(msg, SystemMessage):
                merged_system_message_str += (msg.content if msg.content.strip() else " ") + "\n"
                if first_system_idx == -1:
                    first_system_idx = idx
            else:
                regular_messages.append(msg)
        if merged_system_message_str:
            system_instruction_content = merged_system_message_str.strip()

        for autogen_msg in regular_messages:
            converted_msg_obj = self._convert_message_to_gemini(autogen_msg)
            if isinstance(converted_msg_obj, Content):
                if converted_msg_obj.role not in ["user", "model"]:
                    converted_msg_obj.role = "user"
                gemini_contents.append(converted_msg_obj)

        gemini_tools_converted = self._convert_tools_to_gemini(tools)
        self._last_used_tools = gemini_tools_converted

        logger.info(LLMStreamStartEvent(messages=[msg.model_dump_json() for msg in messages]))

        gen_content_config = genai_types.GenerateContentConfig(
            system_instruction=system_instruction_content if system_instruction_content else None,
            temperature=final_create_args.temperature,
            top_p=final_create_args.top_p,
            top_k=final_create_args.top_k,
            max_output_tokens=final_create_args.max_output_tokens,
            tools=gemini_tools_converted if gemini_tools_converted else None,
        )

        stream_api_task = self._client.aio.models.generate_content_stream(
            model=self._model_name, contents=gemini_contents, config=gen_content_config
        )

        if cancellation_token:
            cancellation_token.link_future(stream_api_task)  # type: ignore

        accumulated_text_parts: List[str] = []

        final_fcs_list: List[FunctionCall] = []

        prompt_tokens_val = 0
        completion_tokens_val = 0
        final_finish_reason: FinishReasons = "unknown"

        try:
            async for chunk in await stream_api_task:
                if chunk.usage_metadata:
                    if chunk.usage_metadata.prompt_token_count:
                        prompt_tokens_val = chunk.usage_metadata.prompt_token_count
                    if chunk.usage_metadata.candidates_token_count:
                        completion_tokens_val = chunk.usage_metadata.candidates_token_count

                if chunk.candidates:
                    candidate_chunk = chunk.candidates[0]
                    if candidate_chunk.finish_reason:
                        final_finish_reason = _normalize_gemini_finish_reason(candidate_chunk.finish_reason)

                    if candidate_chunk.content:
                        for part in candidate_chunk.content.parts:
                            if hasattr(part, "text") and part.text:
                                yield part.text
                                accumulated_text_parts.append(part.text)

                            if hasattr(part, "function_call") and part.function_call:
                                fc_chunk = part.function_call
                                final_fcs_list.append(
                                    FunctionCall(
                                        id=f"call_{normalize_gemini_name(fc_chunk.name)}_{len(final_fcs_list)}",
                                        name=normalize_gemini_name(fc_chunk.name),
                                        arguments=json.dumps(fc_chunk.args) if fc_chunk.args else "{}",
                                    )
                                )

        except Exception as e:
            logger.error(f"Gemini stream API call failed: {e}")
            raise

        usage = RequestUsage(prompt_tokens=prompt_tokens_val, completion_tokens=completion_tokens_val)
        self._total_usage = _add_usage(self._total_usage, usage)
        self._actual_usage = _add_usage(self._actual_usage, usage)

        final_response_content: Union[str, List[FunctionCall]]
        thought_stream: Optional[str] = None

        if final_fcs_list:
            final_response_content = final_fcs_list
            if accumulated_text_parts:
                thought_stream = "".join(accumulated_text_parts)
        else:
            final_response_content = "".join(accumulated_text_parts)
            if final_create_args.response_mime_type == "application/json" and isinstance(final_response_content, str):
                try:
                    json.loads(final_response_content)
                except json.JSONDecodeError:
                    logger.warning("Streamed JSON output was requested, but the final response is not valid JSON.")

        final_result_obj = CreateResult(
            finish_reason=final_finish_reason,
            content=final_response_content,
            usage=usage,
            cached=False,
            thought=thought_stream,
        )

        logger.info(
            LLMStreamEndEvent(
                response=final_result_obj.model_dump(),
                prompt_tokens=usage.prompt_tokens,
                completion_tokens=usage.completion_tokens,
            )
        )
        yield final_result_obj

    async def close(self) -> None:
        await self._client.close()

    def actual_usage(self) -> RequestUsage:
        return self._actual_usage

    def total_usage(self) -> RequestUsage:
        return self._total_usage

    async def count_tokens(self, messages: Sequence[LLMMessage], *, tools: Sequence[Tool | ToolSchema] = []) -> int:
        gemini_contents_for_count: List[Content] = []
        for autogen_msg in messages:
            if not isinstance(autogen_msg, SystemMessage):
                converted_msg_obj = self._convert_message_to_gemini(autogen_msg)
                if isinstance(converted_msg_obj, Content):
                    gemini_contents_for_count.append(converted_msg_obj)

        # TODO: Investigate how Gemini's count_tokens accounts for tools.
        if not gemini_contents_for_count:
            return 0

        try:
            response = self._client.models.count_tokens(model=self._model_name, contents=gemini_contents_for_count)
            return response.total_tokens
        except Exception as e:
            logger.warning(f"Token counting failed: {e}. Returning 0.")
            return 0

    def remaining_tokens(self, messages: Sequence[LLMMessage], *, tools: Sequence[Tool | ToolSchema] = []) -> int:
        token_limit = get_token_limit(self._model_name)
        if not isinstance(token_limit, int) or token_limit <= 0:
            logger.warning(
                f"Cannot calculate remaining tokens: token_limit not available or invalid in model_info for {self._model_name}."
            )
            return 0

        counted_tokens = self.count_tokens(messages, tools=tools)
        return token_limit - counted_tokens

    @property
    def model_info(self) -> ModelInfo:
        return self._model_info

    def __getstate__(self) -> Dict[str, Any]:
        state = self.__dict__.copy()
        state["_client"] = None
        return state

    def __setstate__(self, state: Dict[str, Any]) -> None:
        self.__dict__.update(state)
        resolved_config_from_raw = GeminiVertexAIClientConfiguration(**self._raw_config)

        client_options_dict: Dict[str, Any] = {}
        if resolved_config_from_raw.api_key:
            client_options_dict["api_key"] = resolved_config_from_raw.api_key.get_secret_value()

        if resolved_config_from_raw.vertexai:
            if not resolved_config_from_raw.project or not resolved_config_from_raw.location:
                raise ValueError("project and location are required for Vertex AI.")
            if resolved_config_from_raw.credentials:
                client_options_dict["credentials"] = resolved_config_from_raw.credentials
            else:
                raise ValueError("credentials are required for Vertex AI.")

        self._client = genai.Client(**client_options_dict)

    @property
    def capabilities(self) -> ModelCapabilities:  # type: ignore
        warnings.warn(
            "capabilities is deprecated, use model_info instead",
            DeprecationWarning,
            stacklevel=2,
        )
        return self._model_info

    @classmethod
    def _from_config(cls, config: GeminiVertexAIClientConfiguration) -> "GeminiVertexAIChatCompletionClient":
        copied_config = config.model_copy().model_dump(exclude_none=True)

        if "api_key" in copied_config and isinstance(copied_config["api_key"], str):
            copied_config["api_key"] = SecretStr(copied_config["api_key"])

        return cls(**copied_config)

    def _to_config(self) -> GeminiVertexAIClientConfiguration:
        config_data = self._raw_config.copy()
        return GeminiVertexAIClientConfiguration(**config_data)
