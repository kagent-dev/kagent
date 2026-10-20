"""Exercise HITL pause/resume through the real ADK runner and serialized sessions."""

import json
from collections.abc import AsyncGenerator

import httpx
import pytest
from a2a.server.agent_execution.context import RequestContext
from a2a.server.context import ServerCallContext
from a2a.server.events.event_queue import EventQueue
from a2a.types import DataPart, Message, MessageSendParams, Part, Role, TaskState, TaskStatusUpdateEvent, TextPart
from google.adk.agents import Agent
from google.adk.agents.run_config import RunConfig, StreamingMode
from google.adk.models.base_llm import BaseLlm
from google.adk.models.llm_request import LlmRequest
from google.adk.models.llm_response import LlmResponse
from google.adk.runners import Runner
from google.adk.tools.function_tool import FunctionTool
from google.adk.tools.long_running_tool import LongRunningFunctionTool
from google.genai import types
from kagent.core.a2a import (
    KAGENT_ASK_USER_ANSWERS_KEY,
    KAGENT_HITL_DECISION_TYPE_APPROVE,
    KAGENT_HITL_DECISION_TYPE_KEY,
    KAGENT_HITL_DECISION_TYPE_REJECT,
)
from pydantic import Field

from kagent.adk._agent_executor import A2aAgentExecutor
from kagent.adk._approval import make_approval_callback
from kagent.adk._session_service import KAgentSessionService
from kagent.adk.models._ollama import _convert_content_to_ollama_messages
from kagent.adk.models._openai import _convert_content_to_openai_messages, _convert_content_to_responses_input
from kagent.adk.tools.ask_user_tool import AskUserTool


class ToolCallingModel(BaseLlm):
    model: str = "test-model"
    responses: list[types.Content]
    requests: list[LlmRequest] = Field(default_factory=list)

    async def generate_content_async(
        self, llm_request: LlmRequest, stream: bool = False
    ) -> AsyncGenerator[LlmResponse, None]:
        self.requests.append(llm_request.model_copy(deep=True))
        yield LlmResponse(content=self.responses[len(self.requests) - 1])


@pytest.mark.asyncio
@pytest.mark.parametrize("stream", [False, True])
@pytest.mark.parametrize("case", ["ask_user", "ask_user_invalid", "approve", "reject", "parallel", "long_running"])
async def test_confirmation_survives_pause_and_session_reload(case, stream):
    # Mock the controller HTTP boundary, retaining only JSON between requests.
    # Every get_session therefore reconstructs ADK events just as production does.
    saved_events: list[dict] = []
    session_data = {"id": "session", "user_id": "user"}

    def controller(request: httpx.Request) -> httpx.Response:
        if request.method == "POST" and request.url.path.endswith("/events"):
            saved_events.append(json.loads(request.content))
            return httpx.Response(200, json={"data": {}})
        assert request.method == "GET"
        return httpx.Response(200, json={"data": {"session": session_data, "events": saved_events}})

    executed: list[str] = []

    def delete_file(path: str) -> str:
        """Delete a file after user approval."""
        executed.append(path)
        return "Deleted " + path

    if case in ("ask_user", "ask_user_invalid"):
        calls = [types.FunctionCall(id="ask", name="ask_user", args={"questions": [{"question": "Which database?"}]})]
    else:
        calls = [types.FunctionCall(id="delete-1", name="delete_file", args={"path": "one"})]
        if case == "parallel":
            calls.append(types.FunctionCall(id="delete-2", name="delete_file", args={"path": "two"}))
    responses = [
        types.Content(role="model", parts=[types.Part(function_call=call) for call in calls]),
        types.Content(role="model", parts=[types.Part(text="Finished")]),
    ]
    if case == "ask_user_invalid":
        responses.insert(
            0,
            types.Content(
                role="model",
                parts=[
                    types.Part(
                        function_call=types.FunctionCall(
                            id="invalid-ask", name="ask_user", args={"questions": ["Which database?"]}
                        )
                    )
                ],
            ),
        )
    model = ToolCallingModel(responses=responses)
    pause_requests = 2 if case == "ask_user_invalid" else 1

    async with httpx.AsyncClient(transport=httpx.MockTransport(controller), base_url="http://controller") as client:

        def make_runner():
            return Runner(
                app_name="test-app",
                agent=Agent(
                    name="agent",
                    model=model,
                    tools=[
                        AskUserTool(),
                        LongRunningFunctionTool(delete_file) if case == "long_running" else FunctionTool(delete_file),
                    ],
                    before_tool_callback=None if case == "long_running" else make_approval_callback({"delete_file"}),
                ),
                session_service=KAgentSessionService(client),
            )

        executor = A2aAgentExecutor(runner=make_runner)

        async def invoke(part: Part):
            message = Message(role=Role.user, message_id="message", parts=[part])
            context = RequestContext(
                request=MessageSendParams(message=message),
                task_id="task",
                context_id="session",
                call_context=ServerCallContext(state={}),
            )
            queue = EventQueue()
            runner = make_runner()
            try:
                await executor._handle_request(
                    context,
                    queue,
                    runner,
                    {
                        "user_id": "user",
                        "session_id": "session",
                        "new_message": types.Content(role="user", parts=[types.Part(text="Run the tool")]),
                        "run_config": RunConfig(streaming_mode=StreamingMode.SSE if stream else StreamingMode.NONE),
                    },
                )
            finally:
                await runner.close()
            events = []
            while not queue.queue.empty():
                events.append(await queue.dequeue_event())
            return events

        paused = await invoke(Part(TextPart(text="Run the tool")))
        assert isinstance(paused[-1], TaskStatusUpdateEvent)
        assert paused[-1].final
        assert paused[-1].status.state == TaskState.input_required
        assert paused[-1].status.message is not None
        assert executed == []
        assert len(model.requests) == pause_requests, "The model must not run again before the user responds"
        if case == "ask_user_invalid":
            errors = [
                part.function_response.response
                for content in model.requests[1].contents
                for part in content.parts or []
                if part.function_response and part.function_response.id == "invalid-ask"
            ]
            error = "ask_user: question 1 must be an object"
            assert errors == [{"result": error}]
            contents = model.requests[1].contents
            assert [
                message["content"]
                for message in _convert_content_to_openai_messages(contents)
                if message["role"] == "tool" and message["tool_call_id"] == "invalid-ask"
            ] == [error]
            assert [
                item["output"]
                for item in _convert_content_to_responses_input(contents)
                if item.get("type") == "function_call_output" and item["call_id"] == "invalid-ask"
            ] == [error]
            assert [
                message.content
                for message in _convert_content_to_ollama_messages(contents)
                if message.role == "tool" and message.tool_name == "ask_user"
            ] == [error]

        session = await KAgentSessionService(client).get_session(
            app_name="test-app", user_id="user", session_id="session"
        )
        assert session is not None
        requested_ids = {
            response.id
            for event in session.events
            for response in event.get_function_responses()
            if response.id in event.actions.requested_tool_confirmations
        }
        if case == "long_running":
            # Ordinary long-running calls still pause immediately, without
            # executing the tool or waiting for a confirmation response.
            assert requested_ids == set()
            return
        assert requested_ids == {call.id for call in calls}

        decision = {
            KAGENT_HITL_DECISION_TYPE_KEY: (
                KAGENT_HITL_DECISION_TYPE_REJECT if case == "reject" else KAGENT_HITL_DECISION_TYPE_APPROVE
            )
        }
        if case in ("ask_user", "ask_user_invalid"):
            decision[KAGENT_ASK_USER_ANSWERS_KEY] = [{"answer": ["PostgreSQL"]}]
        resumed = await invoke(Part(DataPart(data=decision)))
        assert isinstance(resumed[-1], TaskStatusUpdateEvent)
        assert resumed[-1].final
        assert resumed[-1].status.state == TaskState.completed
        assert len(model.requests) == pause_requests + 1
        assert executed == (
            [] if case in ("ask_user", "ask_user_invalid", "reject") else [call.args["path"] for call in calls]
        )
        responses = [
            part.function_response
            for content in model.requests[-1].contents
            for part in content.parts or []
            if part.function_response and part.function_response.name == calls[0].name
        ]
        if case in ("ask_user", "ask_user_invalid"):
            assert json.loads(responses[-1].response["result"]) == [
                {"question": "Which database?", "answer": ["PostgreSQL"]}
            ]
        elif case == "reject":
            assert responses[-1].response == {"result": "Tool call was rejected by user."}
        else:
            assert {response.response["result"] for response in responses} >= {
                "Deleted " + call.args["path"] for call in calls
            }
