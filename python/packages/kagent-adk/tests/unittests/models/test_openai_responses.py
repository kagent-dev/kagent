import logging
from unittest import mock

import pytest
from google.adk.models.llm_request import LlmRequest
from google.adk.models.llm_response import LlmResponse
from google.genai import types
from google.genai.types import Content, Part
from openai.types.chat import ChatCompletion, ChatCompletionMessage
from openai.types.chat.chat_completion import Choice
from openai.types.responses import (
    Response,
    ResponseCompletedEvent,
    ResponseError,
    ResponseErrorEvent,
    ResponseFailedEvent,
    ResponseFunctionToolCall,
    ResponseOutputItemDoneEvent,
    ResponseOutputMessage,
    ResponseOutputText,
    ResponseTextDeltaEvent,
    ResponseUsage,
)
from pydantic import ValidationError

from kagent.adk.models import OpenAI
from kagent.adk.models._openai import (
    _convert_content_to_responses_input,
    _convert_responses_output_to_llm_response,
    _convert_tools_to_responses,
)
from kagent.adk.types import OpenAI as OpenAIModelConfig
from kagent.adk.types import _create_llm_from_model_config


def _make_response(output, status="completed", usage=None, error=None) -> Response:
    return Response(
        id="resp_1",
        created_at=0,
        model="gpt-4o",
        object="response",
        output=output,
        parallel_tool_calls=True,
        tool_choice="auto",
        tools=[],
        status=status,
        usage=usage,
        error=error,
    )


@pytest.fixture
def llm_request():
    return LlmRequest(
        model="gpt-4o",
        contents=[Content(role="user", parts=[Part.from_text(text="Hello")])],
        config=types.GenerateContentConfig(
            temperature=0.1,
            response_modalities=[types.Modality.TEXT],
            system_instruction="You are a helpful assistant",
        ),
    )


@pytest.fixture
def responses_llm():
    return OpenAI(model="gpt-4o", type="openai", api_key="fake", api_format="responses", temperature=0.1)


async def _collect_kwargs(llm, llm_request, response):
    """Run a non-streaming call against a mocked client and return the create() kwargs."""
    with mock.patch.object(llm, "_client") as mock_client:

        async def mock_coro(*args, **kwargs):
            return response

        mock_client.responses.create.return_value = mock_coro()

        _ = [resp async for resp in llm.generate_content_async(llm_request, stream=False)]

        mock_client.responses.create.assert_called_once()
        _, kwargs = mock_client.responses.create.call_args
        return kwargs


def test_create_llm_from_model_config_passes_through_api_format():
    model_config = OpenAIModelConfig(model="gpt-4o", type="openai", api_format="responses")
    llm = _create_llm_from_model_config(model_config)
    assert llm.api_format == "responses"


def test_create_llm_from_model_config_defaults_api_format_to_none():
    model_config = OpenAIModelConfig(model="gpt-4o", type="openai")
    llm = _create_llm_from_model_config(model_config)
    assert llm.api_format is None


def test_model_config_rejects_unknown_api_format():
    with pytest.raises(ValidationError):
        OpenAIModelConfig(model="gpt-4o", type="openai", api_format="Responses")


def test_convert_content_to_responses_input_user_message():
    input_items = _convert_content_to_responses_input([Content(role="user", parts=[Part.from_text(text="hello")])])
    assert len(input_items) == 1
    assert input_items[0]["role"] == "user"
    assert input_items[0]["content"] == "hello"


def test_convert_content_to_responses_input_skips_system_role():
    input_items = _convert_content_to_responses_input(
        [Content(role="system", parts=[Part.from_text(text="be helpful")])]
    )
    assert input_items == []


def test_convert_content_to_responses_input_function_call_and_output_paired():
    fc_part = Part.from_function_call(name="add", args={"a": 1, "b": 2})
    fc_part.function_call.id = "call_1"
    fr_part = Part.from_function_response(name="add", response={"result": "3"})
    fr_part.function_response.id = "call_1"

    input_items = _convert_content_to_responses_input(
        [
            Content(role="model", parts=[fc_part]),
            Content(role="user", parts=[fr_part]),
        ]
    )

    assert len(input_items) == 2
    call_item, output_item = input_items
    assert call_item["type"] == "function_call"
    assert call_item["call_id"] == "call_1"
    assert call_item["name"] == "add"
    assert output_item["type"] == "function_call_output"
    assert output_item["call_id"] == "call_1"
    assert output_item["output"] == "3"


def test_convert_content_to_responses_input_function_call_without_response():
    fc_part = Part.from_function_call(name="add", args={"a": 1})
    fc_part.function_call.id = "call_1"

    input_items = _convert_content_to_responses_input([Content(role="model", parts=[fc_part])])

    assert len(input_items) == 2
    assert input_items[1]["output"] == "No response available for this function call."


def test_convert_content_to_responses_input_stringifies_non_string_result():
    fc_part = Part.from_function_call(name="add", args={"a": 1, "b": 2})
    fc_part.function_call.id = "call_1"
    fr_part = Part.from_function_response(name="add", response={"result": 3})
    fr_part.function_response.id = "call_1"

    input_items = _convert_content_to_responses_input(
        [
            Content(role="model", parts=[fc_part]),
            Content(role="user", parts=[fr_part]),
        ]
    )

    assert input_items[1]["output"] == "3"


def test_convert_content_to_responses_input_multimodal_user_message():
    input_items = _convert_content_to_responses_input(
        [
            Content(
                role="user",
                parts=[
                    Part.from_text(text="what is this?"),
                    Part.from_bytes(data=b"\x89PNG", mime_type="image/png"),
                ],
            )
        ]
    )

    assert len(input_items) == 1
    text_item, image_item = input_items[0]["content"]
    assert text_item == {"type": "input_text", "text": "what is this?"}
    assert image_item["type"] == "input_image"
    assert image_item["image_url"].startswith("data:image/png;base64,")


def test_convert_tools_to_responses():
    tool = types.Tool(
        function_declarations=[
            types.FunctionDeclaration(
                name="get_weather",
                description="Gets the weather.",
                parameters=types.Schema(
                    type=types.Type.OBJECT,
                    properties={"location": types.Schema(type=types.Type.STRING)},
                    required=["location"],
                ),
            )
        ]
    )

    result = _convert_tools_to_responses([tool])

    assert len(result) == 1
    assert result[0]["type"] == "function"
    assert result[0]["name"] == "get_weather"
    assert result[0]["parameters"]["properties"]["location"]["type"] == "string"
    assert result[0]["parameters"]["required"] == ["location"]


def test_convert_responses_output_to_llm_response_text():
    response = _make_response(
        output=[
            ResponseOutputMessage(
                id="msg_1",
                content=[ResponseOutputText(type="output_text", text="Hi there!", annotations=[])],
                role="assistant",
                status="completed",
                type="message",
            )
        ],
        usage=ResponseUsage(
            input_tokens=10,
            input_tokens_details={"cache_write_tokens": 0, "cached_tokens": 0},
            output_tokens=5,
            output_tokens_details={"reasoning_tokens": 0},
            total_tokens=15,
        ),
    )

    llm_response = _convert_responses_output_to_llm_response(response)

    assert llm_response.content.parts[0].text == "Hi there!"
    assert llm_response.finish_reason == types.FinishReason.STOP
    assert llm_response.usage_metadata.prompt_token_count == 10
    assert llm_response.usage_metadata.candidates_token_count == 5


def test_convert_responses_output_to_llm_response_function_call():
    response = _make_response(
        output=[
            ResponseFunctionToolCall(
                type="function_call",
                call_id="call_1",
                name="get_weather",
                arguments='{"location": "NYC"}',
            )
        ],
    )

    llm_response = _convert_responses_output_to_llm_response(response)

    fc = llm_response.content.parts[0].function_call
    assert fc.name == "get_weather"
    assert fc.args == {"location": "NYC"}
    assert fc.id == "call_1"


def test_convert_responses_output_to_llm_response_incomplete_status():
    response = _make_response(output=[], status="incomplete")
    llm_response = _convert_responses_output_to_llm_response(response)
    assert llm_response.finish_reason == types.FinishReason.MAX_TOKENS


@pytest.mark.asyncio
async def test_generate_content_async_uses_responses_api(responses_llm, llm_request):
    response = _make_response(
        output=[
            ResponseOutputMessage(
                id="msg_1",
                content=[ResponseOutputText(type="output_text", text="Hi there!", annotations=[])],
                role="assistant",
                status="completed",
                type="message",
            )
        ],
    )

    with mock.patch.object(responses_llm, "_client") as mock_client:

        async def mock_coro(*args, **kwargs):
            return response

        mock_client.responses.create.return_value = mock_coro()

        results = [resp async for resp in responses_llm.generate_content_async(llm_request, stream=False)]

        assert len(results) == 1
        assert isinstance(results[0], LlmResponse)
        assert results[0].content.parts[0].text == "Hi there!"

        mock_client.responses.create.assert_called_once()
        assert mock_client.chat.completions.create.call_count == 0
        _, kwargs = mock_client.responses.create.call_args
        assert kwargs["model"] == "gpt-4o"
        assert kwargs["instructions"] == "You are a helpful assistant"
        assert kwargs["temperature"] == 0.1


@pytest.mark.asyncio
@pytest.mark.parametrize("api_format", [None, "chatCompletions"])
async def test_generate_content_async_defaults_to_chat_completions(api_format, llm_request):
    llm = OpenAI(model="gpt-4o", type="openai", api_key="fake", api_format=api_format)

    completion = ChatCompletion(
        id="chatcmpl_1",
        created=0,
        model="gpt-4o",
        object="chat.completion",
        choices=[
            Choice(
                finish_reason="stop",
                index=0,
                message=ChatCompletionMessage(role="assistant", content="Hi there!"),
            )
        ],
    )

    with mock.patch.object(llm, "_client") as mock_client:

        async def mock_coro(*args, **kwargs):
            return completion

        mock_client.chat.completions.create.return_value = mock_coro()

        results = [resp async for resp in llm.generate_content_async(llm_request, stream=False)]

        assert results[0].content.parts[0].text == "Hi there!"
        mock_client.chat.completions.create.assert_called_once()
        assert mock_client.responses.create.call_count == 0


@pytest.mark.asyncio
async def test_generate_content_async_responses_sends_reasoning_effort(llm_request):
    llm = OpenAI(model="gpt-5", type="openai", api_key="fake", api_format="responses", reasoning_effort="high")

    kwargs = await _collect_kwargs(llm, llm_request, _make_response(output=[]))

    assert kwargs["reasoning"] == {"effort": "high"}


@pytest.mark.asyncio
async def test_generate_content_async_responses_max_completion_tokens_wins(llm_request):
    llm = OpenAI(
        model="gpt-4o",
        type="openai",
        api_key="fake",
        api_format="responses",
        max_tokens=1024,
        max_completion_tokens=2048,
    )

    kwargs = await _collect_kwargs(llm, llm_request, _make_response(output=[]))

    assert kwargs["max_output_tokens"] == 2048
    assert "max_tokens" not in kwargs
    assert "max_completion_tokens" not in kwargs


@pytest.mark.asyncio
async def test_generate_content_async_responses_falls_back_to_max_tokens(llm_request):
    llm = OpenAI(model="gpt-4o", type="openai", api_key="fake", api_format="responses", max_tokens=1024)

    kwargs = await _collect_kwargs(llm, llm_request, _make_response(output=[]))

    assert kwargs["max_output_tokens"] == 1024


@pytest.mark.asyncio
async def test_generate_content_async_responses_sends_tools(responses_llm, llm_request):
    llm_request.config.tools = [
        types.Tool(
            function_declarations=[
                types.FunctionDeclaration(
                    name="get_weather",
                    description="Gets the weather.",
                    parameters=types.Schema(
                        type=types.Type.OBJECT,
                        properties={"location": types.Schema(type=types.Type.STRING)},
                        required=["location"],
                    ),
                )
            ]
        )
    ]

    kwargs = await _collect_kwargs(responses_llm, llm_request, _make_response(output=[]))

    assert kwargs["tool_choice"] == "auto"
    assert [tool["name"] for tool in kwargs["tools"]] == ["get_weather"]


@pytest.mark.asyncio
async def test_generate_content_async_responses_ignored_params_warn(caplog):
    with caplog.at_level(logging.WARNING, logger="kagent.adk.models._openai"):
        OpenAI(model="gpt-5", type="openai", api_key="fake", api_format="responses", seed=7, n=2)

    assert "seed" in caplog.text
    assert "n" in caplog.text


def test_responses_params_do_not_warn_for_chat_completions(caplog):
    with caplog.at_level(logging.WARNING, logger="kagent.adk.models._openai"):
        OpenAI(model="gpt-4o", type="openai", api_key="fake", seed=7)

    assert caplog.text == ""


@pytest.mark.asyncio
async def test_generate_content_async_responses_streaming(responses_llm, llm_request):
    events = [
        ResponseTextDeltaEvent(
            content_index=0,
            delta="Hi ",
            item_id="msg_1",
            logprobs=[],
            output_index=0,
            sequence_number=1,
            type="response.output_text.delta",
        ),
        ResponseTextDeltaEvent(
            content_index=0,
            delta="there!",
            item_id="msg_1",
            logprobs=[],
            output_index=0,
            sequence_number=2,
            type="response.output_text.delta",
        ),
        ResponseOutputItemDoneEvent(
            item=ResponseFunctionToolCall(
                type="function_call",
                call_id="call_1",
                name="get_weather",
                arguments='{"location": "NYC"}',
            ),
            output_index=1,
            sequence_number=3,
            type="response.output_item.done",
        ),
        ResponseCompletedEvent(
            response=_make_response(
                output=[],
                usage=ResponseUsage(
                    input_tokens=1,
                    input_tokens_details={"cache_write_tokens": 0, "cached_tokens": 0},
                    output_tokens=2,
                    output_tokens_details={"reasoning_tokens": 0},
                    total_tokens=3,
                ),
            ),
            sequence_number=4,
            type="response.completed",
        ),
    ]

    with mock.patch.object(responses_llm, "_client") as mock_client:

        async def mock_stream_gen_func(*args, **kwargs):
            async def gen():
                for event in events:
                    yield event

            return gen()

        mock_client.responses.create.side_effect = mock_stream_gen_func

        results = [resp async for resp in responses_llm.generate_content_async(llm_request, stream=True)]

    partials = [r for r in results if r.partial]
    assert [p.content.parts[0].text for p in partials] == ["Hi ", "there!"]

    final = results[-1]
    assert final.partial is False
    assert final.content.parts[0].text == "Hi there!"
    fc = final.content.parts[1].function_call
    assert fc.name == "get_weather"
    assert fc.args == {"location": "NYC"}
    assert final.usage_metadata.prompt_token_count == 1
    assert final.usage_metadata.candidates_token_count == 2


async def _stream_results(llm, llm_request, events):
    with mock.patch.object(llm, "_client") as mock_client:

        async def mock_stream_gen_func(*args, **kwargs):
            async def gen():
                for event in events:
                    yield event

            return gen()

        mock_client.responses.create.side_effect = mock_stream_gen_func

        return [resp async for resp in llm.generate_content_async(llm_request, stream=True)]


@pytest.mark.asyncio
async def test_generate_content_async_responses_streaming_recovers_text_without_deltas(responses_llm, llm_request):
    events = [
        ResponseOutputItemDoneEvent(
            item=ResponseOutputMessage(
                id="msg_1",
                content=[ResponseOutputText(type="output_text", text="Hi there!", annotations=[])],
                role="assistant",
                status="completed",
                type="message",
            ),
            output_index=0,
            sequence_number=1,
            type="response.output_item.done",
        ),
        ResponseCompletedEvent(
            response=_make_response(output=[]),
            sequence_number=2,
            type="response.completed",
        ),
    ]

    results = await _stream_results(responses_llm, llm_request, events)

    assert len(results) == 1
    assert results[0].content.parts[0].text == "Hi there!"
    assert results[0].finish_reason == types.FinishReason.STOP


@pytest.mark.asyncio
async def test_generate_content_async_responses_streaming_surfaces_failure(responses_llm, llm_request):
    events = [
        ResponseFailedEvent(
            response=_make_response(
                output=[],
                status="failed",
                error=ResponseError(code="server_error", message="upstream exploded"),
            ),
            sequence_number=1,
            type="response.failed",
        ),
    ]

    results = await _stream_results(responses_llm, llm_request, events)

    assert len(results) == 1
    assert results[0].error_code == "API_ERROR"
    assert results[0].error_message == "server_error: upstream exploded"


@pytest.mark.asyncio
async def test_generate_content_async_responses_streaming_surfaces_bare_error_event(responses_llm, llm_request):
    events = [
        ResponseErrorEvent(
            code="rate_limit_exceeded",
            message="slow down",
            sequence_number=1,
            type="error",
        ),
    ]

    results = await _stream_results(responses_llm, llm_request, events)

    assert len(results) == 1
    assert results[0].error_code == "API_ERROR"
    assert results[0].error_message == "rate_limit_exceeded: slow down"


@pytest.mark.asyncio
async def test_generate_content_async_responses_surfaces_failed_status(responses_llm, llm_request):
    response = _make_response(
        output=[],
        status="failed",
        error=ResponseError(code="invalid_prompt", message="prompt was rejected"),
    )

    with mock.patch.object(responses_llm, "_client") as mock_client:

        async def mock_coro(*args, **kwargs):
            return response

        mock_client.responses.create.return_value = mock_coro()

        results = [resp async for resp in responses_llm.generate_content_async(llm_request, stream=False)]

    assert len(results) == 1
    assert results[0].error_code == "API_ERROR"
    assert results[0].error_message == "invalid_prompt: prompt was rejected"


@pytest.mark.asyncio
async def test_generate_content_async_responses_failed_status_without_error_payload(responses_llm, llm_request):
    response = _make_response(output=[], status="failed")

    with mock.patch.object(responses_llm, "_client") as mock_client:

        async def mock_coro(*args, **kwargs):
            return response

        mock_client.responses.create.return_value = mock_coro()

        results = [resp async for resp in responses_llm.generate_content_async(llm_request, stream=False)]

    assert results[0].error_code == "API_ERROR"
    assert results[0].error_message == "OpenAI responses request failed"


@pytest.mark.asyncio
async def test_generate_content_async_responses_completed_status_is_not_an_error(responses_llm, llm_request):
    response = _make_response(
        output=[
            ResponseOutputMessage(
                id="msg_1",
                content=[ResponseOutputText(type="output_text", text="Hi there!", annotations=[])],
                role="assistant",
                status="completed",
                type="message",
            )
        ],
    )

    with mock.patch.object(responses_llm, "_client") as mock_client:

        async def mock_coro(*args, **kwargs):
            return response

        mock_client.responses.create.return_value = mock_coro()

        results = [resp async for resp in responses_llm.generate_content_async(llm_request, stream=False)]

    assert results[0].error_code is None
    assert results[0].error_message is None
    assert results[0].content.parts[0].text == "Hi there!"
