"""The invocation span of one A2A request, mirroring go/adk/pkg/a2a/server/tracing.go."""

import asyncio
import importlib.metadata
import logging
from dataclasses import dataclass
from typing import Any

from a2a.server.agent_execution import AgentExecutor, RequestContext
from a2a.server.context import ServerCallContext
from a2a.server.request_handlers import DefaultRequestHandlerV2
from a2a.types import a2a_pb2
from opentelemetry import context as otel_context
from opentelemetry import trace
from opentelemetry.trace import SpanKind, Status, StatusCode

from ..telemetry import _boot
from ..telemetry._conv import (
    A2A_METHOD,
    A2A_TASK_ID,
    A2A_TASK_STATE,
    GEN_AI_CONVERSATION_ID,
    GEN_AI_OPERATION_NAME,
    KAGENT_INVOCATION_DISPOSITION,
    KAGENT_INVOCATION_SEGMENT,
    A2aMethodValues,
    GenAiOperationNameValues,
    KagentInvocationDispositionValues,
    KagentInvocationSegmentValues,
)
from ..telemetry._span_processor import KAGENT_ATTRIBUTES_KEY
from ._requests import KAgentUser

logger = logging.getLogger(__name__)

TRANSPORT_SPAN_NAME = "a2a.request"
ENDUSER_ID = "enduser.id"
ERROR_TYPE = "error.type"

_SCOPE = "kagent.core.a2a"
_INVOCATION_STATE = "kagent.telemetry.invocation"
_PAUSED_STATES = frozenset({a2a_pb2.TASK_STATE_INPUT_REQUIRED, a2a_pb2.TASK_STATE_AUTH_REQUIRED})
_QUIESCENT_STATES = _PAUSED_STATES | {
    a2a_pb2.TASK_STATE_COMPLETED,
    a2a_pb2.TASK_STATE_CANCELED,
    a2a_pb2.TASK_STATE_FAILED,
    a2a_pb2.TASK_STATE_REJECTED,
}


@dataclass(frozen=True)
class Result:
    task_state: str = ""
    disposition: str = ""
    error: str = ""
    detail: str = ""


class Invocation:
    def __init__(self, span: trace.Span):
        self.span = span
        self.context = trace.set_span_in_context(span)
        self.adopted = False
        self.finished = False

    async def end(self, result: Result) -> None:
        await self._end(result, owner=True)

    async def end_transport(self, result: Result) -> None:
        await self._end(result, owner=False)

    async def _end(self, result: Result, owner: bool) -> None:
        if self.finished or (not owner and self.adopted):
            return
        self.finished = True
        attributes = {
            A2A_TASK_STATE: result.task_state,
            KAGENT_INVOCATION_DISPOSITION: result.disposition,
            ERROR_TYPE: result.error,
        }
        self.span.set_attributes({key: value for key, value in attributes.items() if value})
        if result.error:
            message = f"{result.error}: {result.detail}" if result.detail else result.error
            self.span.set_status(Status(StatusCode.ERROR, message))
        self.span.end()
        if _boot.current() is not None:
            await asyncio.to_thread(_boot.force_flush)


def _tracer() -> trace.Tracer:
    try:
        version = importlib.metadata.version("kagent-core")
    except importlib.metadata.PackageNotFoundError:
        version = ""
    return trace.get_tracer(_SCOPE, version, schema_url=_boot.SCHEMA_URL)


def _user_id(call_context: ServerCallContext | None) -> str:
    if call_context is None:
        return ""
    if isinstance(call_context.user, KAgentUser):
        return call_context.user.user_name
    headers = call_context.state.get("headers") or {}
    return headers.get("x-user-id") or ""


def _task_state(event: Any) -> int | None:
    if isinstance(event, (a2a_pb2.Task, a2a_pb2.TaskStatusUpdateEvent)):
        return event.status.state
    return None


def _quiescent(event: Any) -> bool:
    return isinstance(event, a2a_pb2.Message) or _task_state(event) in _QUIESCENT_STATES


def _state_name(event: Any) -> str:
    state = _task_state(event)
    return a2a_pb2.TaskState.Name(state) if state is not None else ""


def _start(method: str, call_context: ServerCallContext) -> Invocation:
    attributes: dict[str, str] = {A2A_METHOD: method}
    name = TRANSPORT_SPAN_NAME
    providers = _boot.current()
    if providers is not None:
        attributes.update(providers.identity.attributes())
        if not providers.profile.emits_invoke_agent:
            operation = GenAiOperationNameValues.INVOKE_AGENT.value
            name = f"{operation} {providers.identity.agent_name}".strip()
            attributes[GEN_AI_OPERATION_NAME] = operation
    if user := _user_id(call_context):
        attributes[ENDUSER_ID] = user
    span = _tracer().start_span(
        name,
        kind=SpanKind.INTERNAL,
        attributes=attributes,
        record_exception=False,
        set_status_on_exception=False,
    )
    invocation = Invocation(span)
    call_context.state[_INVOCATION_STATE] = invocation
    return invocation


def request_attributes(context: RequestContext) -> dict[str, str]:
    """Request identity for the invocation span and every span beneath it."""
    attributes: dict[str, str] = {}
    if context.context_id:
        attributes[GEN_AI_CONVERSATION_ID] = context.context_id
    if context.task_id:
        attributes[A2A_TASK_ID] = context.task_id
    if user := _user_id(context.call_context):
        attributes[ENDUSER_ID] = user
    return attributes


def _segment(context: RequestContext) -> str:
    task = context.current_task
    if task is not None and task.status.state in _PAUSED_STATES:
        return KagentInvocationSegmentValues.RESUMED.value
    return KagentInvocationSegmentValues.INITIAL.value


class _InvocationQueue:
    def __init__(self, events, invocation: Invocation):
        self._events = events
        self._invocation = invocation

    async def enqueue_event(self, event) -> None:
        if _quiescent(event):
            await self._invocation.end(Result(task_state=_state_name(event)))
        await self._events.enqueue_event(event)

    def __getattr__(self, name):
        return getattr(self._events, name)


class TelemetryExecutor(AgentExecutor):
    """Adopt the request's invocation span for the execution it starts."""

    def __init__(self, executor: AgentExecutor):
        self.executor = executor

    async def execute(self, context: RequestContext, event_queue) -> None:
        state = context.call_context.state if context.call_context else {}
        invocation = state.get(_INVOCATION_STATE)
        if not isinstance(invocation, Invocation) or invocation.finished:
            await self.executor.execute(context, event_queue)
            return
        invocation.adopted = True
        identity = request_attributes(context)
        invocation.span.set_attributes({**identity, KAGENT_INVOCATION_SEGMENT: _segment(context)})
        token = otel_context.attach(otel_context.set_value(KAGENT_ATTRIBUTES_KEY, identity, invocation.context))
        try:
            await self.executor.execute(context, _InvocationQueue(event_queue, invocation))
        except asyncio.CancelledError:
            await invocation.end(Result(disposition=KagentInvocationDispositionValues.CANCELED.value))
            raise
        except Exception as error:
            logger.exception("Agent execution failed")
            await invocation.end(Result(error="runtime_error", detail=type(error).__qualname__))
            raise
        finally:
            otel_context.detach(token)
            await invocation.end(Result())

    async def cancel(self, context: RequestContext, event_queue) -> None:
        await self.executor.cancel(context, event_queue)


class TelemetryRequestHandlerMixin:
    """Open the invocation span around message sends and close it when execution never adopts it."""

    async def on_message_send(self, params, context: ServerCallContext):
        invocation = _start(A2aMethodValues.SEND_MESSAGE.value, context)
        token = otel_context.attach(invocation.context)
        try:
            result = await super().on_message_send(params, context)
        except asyncio.CancelledError:
            await invocation.end_transport(Result(disposition=KagentInvocationDispositionValues.ABANDONED.value))
            raise
        except Exception as error:
            await invocation.end_transport(Result(error="transport_error", detail=type(error).__qualname__))
            raise
        finally:
            otel_context.detach(token)
        await invocation.end_transport(Result(task_state=_state_name(result)))
        return result

    async def on_message_send_stream(self, params, context: ServerCallContext):
        invocation = _start(A2aMethodValues.SEND_STREAMING_MESSAGE.value, context)
        events = super().on_message_send_stream(params, context)
        try:
            token = otel_context.attach(invocation.context)
            try:
                event = await anext(events, None)
            finally:
                otel_context.detach(token)
            while event is not None:
                if _quiescent(event):
                    await invocation.end_transport(Result(task_state=_state_name(event)))
                yield event
                event = await anext(events, None)
        except (asyncio.CancelledError, GeneratorExit):
            await invocation.end_transport(Result(disposition=KagentInvocationDispositionValues.ABANDONED.value))
            raise
        except Exception as error:
            await invocation.end_transport(Result(error="transport_error", detail=type(error).__qualname__))
            raise
        finally:
            await events.aclose()
            await invocation.end_transport(Result())


class TelemetryRequestHandler(TelemetryRequestHandlerMixin, DefaultRequestHandlerV2):
    def __init__(self, *, agent_executor: AgentExecutor, **kwargs):
        super().__init__(agent_executor=TelemetryExecutor(agent_executor), **kwargs)
