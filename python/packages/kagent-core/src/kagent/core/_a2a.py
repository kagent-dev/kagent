import logging

from a2a.auth.user import User
from a2a.server.agent_execution import RequestContext, SimpleRequestContextBuilder
from a2a.server.context import ServerCallContext
from a2a.server.tasks import TaskStore
from a2a.types import MessageSendParams, Task

# --- Constants ---
DEFAULT_USER_ID = "admin@kagent.dev"

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
    """Request context builder that injects user information for KAgent."""

    def __init__(self, user_id: str, task_store: TaskStore):
        """Initialize the context builder.

        Args:
            user_id: Default user ID to use
            task_store: Task store implementation
        """
        super().__init__(task_store=task_store)
        self.user_id = user_id

    async def build(
        self,
        params: MessageSendParams | None = None,
        task_id: str | None = None,
        context_id: str | None = None,
        task: Task | None = None,
        context: ServerCallContext | None = None,
    ) -> RequestContext:
        """Build a request context with user information."""
        if not context:
            context = ServerCallContext(user=KAgentUser(user_id=self.user_id))
        else:
            context.user = KAgentUser(user_id=self.user_id)

        request_context = await super().build(params, task_id, context_id, task, context)
        return request_context
