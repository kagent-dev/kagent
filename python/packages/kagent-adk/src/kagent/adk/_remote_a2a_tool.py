"""Remote A2A agent tool with HITL propagation.

Replaces the upstream ``AgentTool(RemoteA2aAgent(...))`` pairing with a single
``BaseTool`` that directly manages the A2A conversation with a remote agent.

When the remote agent returns ``TaskState.input_required`` (i.e. one of its
tools needs human approval), this tool calls ``request_confirmation()`` to
surface the HITL prompt to the parent agent's flow. On resume the user's
decision is forwarded to the remote agent's pending task.

This is a BaseToolset wrapper around KAgentRemoteA2ATool for runner cleanup purposes.
"""

import logging
import uuid
from typing import Any, Callable, Optional, Protocol, runtime_checkable
from urllib.parse import urlparse

import httpx
from a2a.client import Client as A2AClient
from a2a.client.card_resolver import A2ACardResolver
from a2a.client.client import ClientConfig as A2AClientConfig
from a2a.client.client_factory import ClientFactory as A2AClientFactory
from a2a.client.errors import A2AClientHTTPError
from a2a.client.middleware import ClientCallContext, ClientCallInterceptor
from a2a.types import (
    AgentCard,
    DataPart,
    Role,
    Task,
    TaskState,
    TextPart,
)
from a2a.types import (
    Message as A2AMessage,
)
from a2a.types import (
    Part as A2APart,
)
from a2a.types import (
    TransportProtocol as A2ATransport,
)
from google.adk.agents.readonly_context import ReadonlyContext
from google.adk.tools.base_tool import BaseTool
from google.adk.tools.base_toolset import BaseToolset
from google.adk.tools.tool_context import ToolContext
from google.genai import types as genai_types
from kagent.core.a2a import (
    KAGENT_HITL_DECISION_TYPE_APPROVE,
    KAGENT_HITL_DECISION_TYPE_BATCH,
    KAGENT_HITL_DECISION_TYPE_KEY,
    KAGENT_HITL_DECISION_TYPE_REJECT,
    extract_hitl_info_from_task,
)

logger = logging.getLogger("kagent_adk." + __name__)

_USER_ID_CONTEXT_KEY = "x-user-id"
_SOURCE_HEADER = "x-kagent-source"
_SOURCE_SUBAGENT = "agent"
_HEADERS_STATE_KEY = "headers"
_EXTRA_HEADERS_CONTEXT_KEY = "_a2a_extra_headers"


class _SubagentInterceptor(ClientCallInterceptor):
    """
    Injects the authenticated user's ID as an ``x-user-id`` HTTP header,
    marks the request as originating from an agent call via
    ``x-kagent-source: agent``, and forwards any pre-computed propagation
    headers stored in the call context state under ``_EXTRA_HEADERS_CONTEXT_KEY``.
    """

    async def intercept(self, method_name, request_payload, http_kwargs, agent_card, context):
        headers = dict(http_kwargs.get("headers", {}))
        headers[_SOURCE_HEADER] = _SOURCE_SUBAGENT

        if context:
            if _USER_ID_CONTEXT_KEY in context.state:
                headers["x-user-id"] = context.state[_USER_ID_CONTEXT_KEY]
            extra = context.state.get(_EXTRA_HEADERS_CONTEXT_KEY)
            if extra:
                headers.update(extra)
        http_kwargs["headers"] = headers
        return request_payload, http_kwargs


def _extract_text_from_task(task: Task) -> str:
    """Extract text content from a completed task's artifacts or status message."""
    # Prefer artifacts (the canonical result)
    if task.artifacts:
        texts: list[str] = []
        for artifact in task.artifacts:
            if artifact.parts:
                for part in artifact.parts:
                    root = part.root if hasattr(part, "root") else part
                    if isinstance(root, TextPart) and root.text:
                        texts.append(root.text)
        if texts:
            return "\n".join(texts)

    # Fall back to status message
    if task.status and task.status.message and task.status.message.parts:
        texts = []
        for part in task.status.message.parts:
            root = part.root if hasattr(part, "root") else part
            if isinstance(root, TextPart) and root.text:
                texts.append(root.text)
        if texts:
            return "\n".join(texts)

    return ""


def _extract_usage_from_task(task: Task) -> Optional[dict]:
    """Extract kagent_usage_metadata from a completed task.

    The A2A task_manager merges the final TaskStatusUpdateEvent.metadata into
    task.metadata. The agent executor now adds the last LLM invocation's
    usage_metadata to run_metadata before publishing the final event, so it
    is available here for non-streaming callers like KAgentRemoteA2ATool.
    """
    if task.metadata:
        usage = task.metadata.get("kagent_usage_metadata")
        if usage and isinstance(usage, dict):
            return usage
    return None


@runtime_checkable
class SubagentSessionProvider(Protocol):
    """Protocol for tools that delegate to a subagent and can expose
    the subagent's session ID for live activity polling."""

    name: str

    @property
    def subagent_session_id(self) -> str | None: ...


class KAgentRemoteA2ATool(BaseTool):
    """A tool that calls a remote A2A agent and propagates HITL state."""

    def __init__(
        self,
        *,
        name: str,
        description: str,
        agent_card_url: str,
        httpx_client: Optional[httpx.AsyncClient] = None,
        header_provider: Optional[Callable[[Optional[ReadonlyContext]], dict[str, str]]] = None,
    ) -> None:
        super().__init__(name=name, description=description)
        self._agent_card_url = agent_card_url
        self._httpx_client = httpx_client
        self._header_provider = header_provider
        self._a2a_client: Optional[A2AClient] = None
        self._agent_card: Optional[AgentCard] = None
        # Pre-generate context_id for UI session polling
        self._last_context_id: str = str(uuid.uuid4())

    @property
    def subagent_session_id(self) -> str | None:
        """The subagent's session ID (== context_id sent in the A2A message)."""
        return self._last_context_id

    async def _ensure_client(self) -> A2AClient:
        """Lazily resolve the agent card and initialize the A2A client."""
        if self._a2a_client is not None:
            return self._a2a_client

        if self._httpx_client is None:
            raise RuntimeError(
                f"No httpx client provided for remote A2A tool '{self.name}'. "
                "Use KAgentRemoteA2AToolset to manage the client lifecycle."
            )

        # Resolve the agent card from URL
        parsed = urlparse(self._agent_card_url)
        base_url = f"{parsed.scheme}://{parsed.netloc}"
        resolver = A2ACardResolver(httpx_client=self._httpx_client, base_url=base_url)
        self._agent_card = await resolver.get_agent_card(relative_card_path=parsed.path)

        if not self._agent_card.url:
            raise ValueError(f"Agent card for {self.name} has no RPC URL")

        # Auto-populate description from agent card if we don't have one
        if not self.description and self._agent_card.description:
            self.description = self._agent_card.description

        # Create the A2A client.
        config = A2AClientConfig(
            httpx_client=self._httpx_client,
            streaming=False,
            polling=False,
            supported_transports=[A2ATransport.jsonrpc],
        )
        factory = A2AClientFactory(config=config)
        self._a2a_client = factory.create(
            self._agent_card,
            interceptors=[_SubagentInterceptor()],
        )
        return self._a2a_client

    def _get_declaration(self) -> genai_types.FunctionDeclaration:
        """Same schema as AgentTool for agents without an input schema."""
        return genai_types.FunctionDeclaration(
            name=self.name,
            description=self.description,
            parameters=genai_types.Schema(
                type=genai_types.Type.OBJECT,
                properties={
                    "request": genai_types.Schema(type=genai_types.Type.STRING),
                },
                required=["request"],
            ),
        )

    def _build_call_context(self, tool_context: ToolContext) -> ClientCallContext:
        state: dict[str, Any] = {_USER_ID_CONTEXT_KEY: tool_context.session.user_id}
        if self._header_provider:
            extra_headers = self._header_provider(tool_context)
            if extra_headers:
                state[_EXTRA_HEADERS_CONTEXT_KEY] = extra_headers
        return ClientCallContext(state=state)

    async def run_async(self, *, args: dict[str, Any], tool_context: ToolContext) -> Any:
        """Execute the remote agent tool.

        Phase 1 (first invocation): Send the request to the remote agent.
          - If completed: return the result text.
          - If input_required: call request_confirmation() to pause and
            propagate the HITL prompt to the parent. Store subagent task_id
            and context_id in the confirmation payload.
          - If failed: return the error message.

        Phase 2 (resume after HITL): Forward the user's decision to the
        remote agent's pending task and return the final result.
        """
        if tool_context.tool_confirmation is not None:
            return await self._handle_resume(tool_context)

        return await self._handle_first_call(args, tool_context)

    async def _handle_first_call(self, args: dict[str, Any], tool_context: ToolContext) -> Any:
        """Phase 1: Send the request to the remote agent."""
        client = await self._ensure_client()

        request_text = args.get("request", "")
        message = A2AMessage(
            message_id=str(uuid.uuid4()),
            parts=[A2APart(root=TextPart(text=request_text))],
            role=Role.user,
            # Pass context_id for session continuity with stateful remote agents
            context_id=self._last_context_id,
        )

        # Forward the authenticated user ID so the subagent session is scoped
        # to the same user as the parent agent session.
        call_context = self._build_call_context(tool_context)

        task: Optional[Task] = None
        try:
            async for response in client.send_message(request=message, context=call_context):
                if isinstance(response, tuple):
                    # ClientEvent: (Task, UpdateEvent | None)
                    task = response[0]
                elif isinstance(response, A2AMessage):
                    return self._extract_text_from_message(response)
        except A2AClientHTTPError as e:
            return f"Remote agent '{self.name}' request failed: {e}"
        except Exception as e:
            logger.error("Error calling remote agent %s: %s", self.name, e, exc_info=True)
            return f"Remote agent '{self.name}' request failed: {e}"

        if task is None:
            return f"Remote agent '{self.name}' returned no result."

        state = task.status.state if task.status else None

        if state == TaskState.input_required:
            return self._handle_input_required(task, tool_context)

        if state == TaskState.failed:
            error_text = _extract_text_from_task(task)
            return error_text or f"Remote agent '{self.name}' failed."

        # completed — include the sub-agent's final LLM usage from task.metadata
        # so the parent can display it on the AgentCall card in the UI.
        result_text = _extract_text_from_task(task)
        usage = _extract_usage_from_task(task)
        if usage:
            return {"result": result_text, "kagent_usage_metadata": usage, "subagent_session_id": self._last_context_id}
        return {"result": result_text or "", "subagent_session_id": self._last_context_id}

    def _handle_input_required(self, task: Task, tool_context: ToolContext) -> dict[str, Any]:
        """Handle a subagent that returned input_required (HITL).

        Calls request_confirmation() to pause the parent agent and surface
        the HITL prompt to the UI.  The subagent's task_id and context_id
        are stored in the confirmation payload so the resume path can
        forward the user's decision.
        """
        hitl_parts = extract_hitl_info_from_task(task)

        # Build a human-readable hint describing what the subagent is waiting for
        inner_tool_names: list[str] = []
        if hitl_parts:
            for hp in hitl_parts:
                if hp.tool_name:
                    inner_tool_names.append(hp.tool_name)

        if inner_tool_names:
            hint = f"Remote agent '{self.name}' requires approval for tool(s): {', '.join(inner_tool_names)}"
        else:
            hint = f"Remote agent '{self.name}' requires human input before continuing."

        # Serialize HitlPartInfo models to dicts for the payload
        payload = {
            "task_id": task.id,
            "context_id": task.context_id,
            "subagent_name": self.name,
            "hitl_parts": [hp.model_dump(by_alias=True) for hp in hitl_parts] if hitl_parts else None,
        }

        logger.info(
            "Subagent %s returned input_required (task=%s), requesting confirmation from parent",
            self.name,
            task.id,
        )

        tool_context.request_confirmation(hint=hint, payload=payload)
        return {"status": "pending", "waiting_for": "subagent_approval", "subagent": self.name}

    async def _handle_resume(self, tool_context: ToolContext) -> Any:
        """Phase 2: Forward the user's decision to the remote agent's pending task."""
        confirmation = tool_context.tool_confirmation
        payload = confirmation.payload or {}

        task_id = payload.get("task_id")
        context_id = payload.get("context_id")
        subagent_name = payload.get("subagent_name", self.name)

        if not task_id:
            logger.error("Resume for %s but no task_id in confirmation payload", self.name)
            return f"Cannot resume remote agent '{subagent_name}': missing task context."

        # Build the decision message.
        # The parent executor merges its own data into the payload alongside
        # the original request_confirmation payload (task_id, context_id, etc).
        # We detect the kind of decision and forward the relevant keys.
        decision_type = None
        batch_decisions = payload.get("batch_decisions")
        ask_user_answers = payload.get("answers")

        if batch_decisions and isinstance(batch_decisions, dict):
            # Per-tool batch decisions (mixed approve/reject for inner tools)
            decision_type = KAGENT_HITL_DECISION_TYPE_BATCH
            decision_data: dict[str, Any] = {
                KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_BATCH,
                "decisions": batch_decisions,
            }
            rej_reasons = payload.get("rejection_reasons")
            if rej_reasons and isinstance(rej_reasons, dict):
                decision_data["rejection_reasons"] = rej_reasons
        elif ask_user_answers and isinstance(ask_user_answers, list):
            # ask_user answers — forward as approve + answers so the
            # subagent's _process_hitl_decision takes the ask_user path
            decision_type = KAGENT_HITL_DECISION_TYPE_APPROVE
            decision_data = {
                KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_APPROVE,
                "ask_user_answers": ask_user_answers,
            }
        else:
            if confirmation.confirmed:
                decision_type = KAGENT_HITL_DECISION_TYPE_APPROVE
            else:
                decision_type = KAGENT_HITL_DECISION_TYPE_REJECT
            decision_data = {KAGENT_HITL_DECISION_TYPE_KEY: decision_type}
            # Include rejection reason if available
            if not confirmation.confirmed and payload:
                reason = payload.get("rejection_reason")
                if reason:
                    decision_data["rejection_reason"] = reason

        decision_message = A2AMessage(
            message_id=str(uuid.uuid4()),
            task_id=task_id,
            context_id=context_id,
            role=Role.user,
            parts=[A2APart(root=DataPart(data=decision_data))],
        )

        logger.info(
            "Forwarding %s decision to subagent %s task %s",
            decision_type,
            subagent_name,
            task_id,
        )

        client = await self._ensure_client()
        call_context = self._build_call_context(tool_context)
        task: Optional[Task] = None
        try:
            async for response in client.send_message(request=decision_message, context=call_context):
                if isinstance(response, tuple):
                    task = response[0]
                elif isinstance(response, A2AMessage):
                    return self._extract_text_from_message(response)
        except A2AClientHTTPError as e:
            return f"Remote agent '{subagent_name}' resume failed: {e}"
        except Exception as e:
            logger.error("Error resuming remote agent %s: %s", subagent_name, e, exc_info=True)
            return f"Remote agent '{subagent_name}' resume failed: {e}"

        if task is None:
            return f"Remote agent '{subagent_name}' returned no result after resume."

        state = task.status.state if task.status else None

        if state == TaskState.input_required:
            # The subagent has another HITL request (e.g. multiple tools needing
            # approval in sequence). Surface it again.
            return self._handle_input_required(task, tool_context)

        if state == TaskState.failed:
            error_text = _extract_text_from_task(task)
            return error_text or f"Remote agent '{subagent_name}' failed after resume."

        result_text = _extract_text_from_task(task)
        usage = _extract_usage_from_task(task)
        if usage:
            return {
                "result": result_text,
                "kagent_usage_metadata": usage,
                "subagent_session_id": context_id or self._last_context_id,
            }
        # context_id from the confirmation payload is the original subagent session ID in case of interrupts
        return {"result": result_text, "subagent_session_id": context_id or self._last_context_id}

    @staticmethod
    def _extract_text_from_message(message: A2AMessage) -> str:
        """Extract text from a direct A2A Message response."""
        if not message.parts:
            return ""
        texts: list[str] = []
        for part in message.parts:
            root = part.root if hasattr(part, "root") else part
            if isinstance(root, TextPart) and root.text:
                texts.append(root.text)
        return "\n".join(texts)


class KAgentRemoteA2AToolset(BaseToolset):
    """A ``BaseToolset`` wrapper around ``KAgentRemoteA2ATool``.

    ADK's ``Runner.close()`` only discovers and closes ``BaseToolset`` instances
    (via ``_collect_toolset``), not bare ``BaseTool`` instances.  By wrapping
    the tool in this toolset the httpx client is guaranteed to be closed when
    the runner shuts down, preventing connection leaks across many agent runs.
    """

    def __init__(
        self,
        *,
        name: str,
        description: str,
        agent_card_url: str,
        httpx_client: httpx.AsyncClient,
        header_provider: Optional[Callable[[Optional[ReadonlyContext]], dict[str, str]]] = None,
    ) -> None:
        super().__init__()
        self._httpx_client = httpx_client
        self._tool = KAgentRemoteA2ATool(
            name=name,
            description=description,
            agent_card_url=agent_card_url,
            httpx_client=httpx_client,
            header_provider=header_provider,
        )

    @property
    def name(self) -> str:
        return self._tool.name

    @property
    def subagent_session_id(self) -> str | None:
        """The subagent's session ID (== context_id sent in the A2A message)."""
        return self._tool.subagent_session_id

    async def get_tools(self, readonly_context: Optional[ReadonlyContext] = None) -> list[BaseTool]:
        return [self._tool]

    async def close(self) -> None:
        """Close the httpx client owned by this toolset."""
        if self._httpx_client is not None:
            try:
                await self._httpx_client.aclose()
                logger.debug("Closed httpx client for remote A2A toolset %s", self._tool.name)
            except Exception as e:
                logger.warning(
                    "Failed to close httpx client for remote A2A toolset %s: %s",
                    self._tool.name,
                    e,
                )
            finally:
                self._httpx_client = None
