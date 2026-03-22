"""KAgentApp for AG2 — builds a FastAPI application."""

import logging
from collections.abc import Callable

from a2a.server.request_handling import DefaultRequestHandler
from a2a.server.tasks import InMemoryTaskStore
from a2a.types import AgentCard
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from kagent.core import (
    KAgentConfig,
    KAgentRequestContextBuilder,
    KAgentTaskStore,
    configure_tracing,
)

from ._executor import AG2AgentExecutor

try:
    from a2a.server.apps import A2AFastAPIApplication
except ImportError:
    from a2a.server.apps import A2AStarletteApplication as A2AFastAPIApplication

logger = logging.getLogger(__name__)


class KAgentApp:
    """Builds a FastAPI app wrapping an AG2 multi-agent group chat.

    Args:
        pattern_factory: Callable returning a fresh AG2 Pattern
            per request.
        agent_card: A2A AgentCard dict or AgentCard instance.
        max_rounds: Max conversation rounds per request.
        config: Optional KAgentConfig (reads from env if None).
    """

    def __init__(
        self,
        pattern_factory: Callable,
        agent_card: dict | AgentCard,
        max_rounds: int = 20,
        config: KAgentConfig | None = None,
    ):
        self._pattern_factory = pattern_factory
        self._max_rounds = max_rounds
        self._config = config or KAgentConfig()

        if isinstance(agent_card, dict):
            self._agent_card = AgentCard(**agent_card)
        else:
            self._agent_card = agent_card

    def build(self) -> FastAPI:
        """Build and return the FastAPI application."""
        executor = AG2AgentExecutor(
            pattern_factory=self._pattern_factory,
            max_rounds=self._max_rounds,
        )

        # Use persistent task store if kagent backend is
        # available, otherwise in-memory
        try:
            task_store = KAgentTaskStore(
                url=self._config.url,
                app_name=self._config.app_name,
            )
        except (ConnectionError, OSError, ValueError) as e:
            logger.warning(
                "kagent backend not available (%s), using "
                "in-memory task store",
                e,
            )
            task_store = InMemoryTaskStore()

        request_handler = DefaultRequestHandler(
            agent_executor=executor,
            task_store=task_store,
            context_builder=KAgentRequestContextBuilder(),
        )

        a2a_app = A2AFastAPIApplication(
            agent_card=self._agent_card,
            http_handler=request_handler,
        )

        app = FastAPI(title=self._agent_card.name)

        configure_tracing(self._config.name, self._config.namespace, app)

        app.add_middleware(
            CORSMiddleware,
            allow_origins=["*"],
            allow_methods=["*"],
            allow_headers=["*"],
        )

        @app.get("/health")
        async def health():
            return {"status": "ok"}

        a2a_app.mount(app)
        return app

    def build_local(self) -> FastAPI:
        """Build app with in-memory task store (no kagent
        backend required)."""
        executor = AG2AgentExecutor(
            pattern_factory=self._pattern_factory,
            max_rounds=self._max_rounds,
        )

        task_store = InMemoryTaskStore()
        request_handler = DefaultRequestHandler(
            agent_executor=executor,
            task_store=task_store,
        )

        a2a_app = A2AFastAPIApplication(
            agent_card=self._agent_card,
            http_handler=request_handler,
        )

        app = FastAPI(title=self._agent_card.name)

        @app.get("/health")
        async def health():
            return {"status": "ok"}

        a2a_app.mount(app)
        return app
