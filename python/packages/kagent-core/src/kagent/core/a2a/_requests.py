import logging

import grpc
from a2a.auth.user import User
from a2a.server.agent_execution import RequestContext, SimpleRequestContextBuilder
from a2a.server.context import ServerCallContext
from a2a.server.request_handlers.grpc_handler import DefaultGrpcServerCallContextBuilder
from a2a.server.tasks import TaskStore
from a2a.types import SendMessageRequest, Task

from ._context import set_request_user_id

# --- Configure Logging ---
logger = logging.getLogger(__name__)


class KAgentUser(User):
    """A simple user implementation for KAgent integration."""

    def __init__(self, user_id: str):
        self.user_id = user_id

    @property
    def is_authenticated(self) -> bool:
        return False

    @property
    def user_name(self) -> str:
        return self.user_id


def _apply_kagent_headers(context: ServerCallContext) -> None:
    headers = context.state.get("headers", {})
    user_id = headers.get("x-user-id")
    if user_id:
        context.user = KAgentUser(user_id=user_id)
        set_request_user_id(user_id)
    source = headers.get("x-kagent-source")
    if source:
        context.state["kagent_source"] = source


class KAgentGrpcServerCallContextBuilder(DefaultGrpcServerCallContextBuilder):
    """Preserve gateway metadata in upstream A2A gRPC call contexts."""

    def build(self, context: grpc.aio.ServicerContext) -> ServerCallContext:
        call_context = super().build(context)
        call_context.state["headers"] = {key.lower(): value for key, value in context.invocation_metadata()}
        _apply_kagent_headers(call_context)
        return call_context


class KAgentRequestContextBuilder(SimpleRequestContextBuilder):
    """
    A request context builder that will be used to hack in the user_id for now.
    """

    def __init__(self, task_store: TaskStore):
        super().__init__(task_store=task_store)

    async def build(
        self,
        context: ServerCallContext,
        params: SendMessageRequest | None = None,
        task_id: str | None = None,
        context_id: str | None = None,
        task: Task | None = None,
    ) -> RequestContext:
        if context:
            _apply_kagent_headers(context)
        request_context = await super().build(
            context=context,
            params=params,
            task_id=task_id,
            context_id=context_id,
            task=task,
        )
        return request_context
