import logging

from a2a.auth.user import User
from a2a.server.agent_execution import RequestContext, SimpleRequestContextBuilder
from a2a.server.context import ServerCallContext
from a2a.server.tasks import TaskStore
from a2a.types import MessageSendParams, Task

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


class KAgentRequestContextBuilder(SimpleRequestContextBuilder):
    """
    A request context builder that will be used to hack in the user_id for now.
    """

    def __init__(self, task_store: TaskStore):
        super().__init__(task_store=task_store)

    async def build(
        self,
        params: MessageSendParams | None = None,
        task_id: str | None = None,
        context_id: str | None = None,
        task: Task | None = None,
        context: ServerCallContext | None = None,
    ) -> RequestContext:
        if context:
            # grab the user id from the header
            headers = context.state.get("headers", {})
            user_id = headers.get("x-user-id", None)
            if user_id:
                context.user = KAgentUser(user_id=user_id)
        request_context = await super().build(params, task_id, context_id, task, context)
        return request_context
