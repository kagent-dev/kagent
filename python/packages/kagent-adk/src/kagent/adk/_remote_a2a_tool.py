"""Remote A2A agent tool with HITL propagation.

Replaces the upstream ``AgentTool(RemoteA2aAgent(...))`` pairing with a single
``BaseTool`` that directly manages the A2A conversation with a remote agent.

When the remote agent returns ``TaskState.input_required`` (i.e. one of its
tools needs human approval), this tool calls ``request_confirmation()`` to
surface the HITL prompt to the parent agent's flow. On resume the user's
decision is forwarded to the remote agent's pending task.
"""

import logging
import uuid
from typing import Any, Optional
from urllib.parse import urlparse

import httpx
from a2a.client import Client as A2AClient
from a2a.client.card_resolver import A2ACardResolver
from a2a.client.client import ClientConfig as A2AClientConfig
from a2a.client.client_factory import ClientFactory as A2AClientFactory
from a2a.client.errors import A2AClientHTTPError
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
from google.adk.tools.base_tool import BaseTool
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


class KAgentRemoteA2ATool(BaseTool):
    """A tool that calls a remote A2A agent and propagates HITL state.

    Unlike the upstream ``AgentTool(RemoteA2aAgent(...))`` pairing, this tool
    has direct access to ``ToolContext`` and can use ``request_confirmation()``
    to surface subagent HITL prompts to the parent agent's approval flow.
    """

    def __init__(
        self,
        *,
        name: str,
        description: str,
        agent_card_url: str,
        httpx_client: Optional[httpx.AsyncClient] = None,
    ) -> None:
        super().__init__(name=name, description=description)
        self._agent_card_url = agent_card_url
        self._httpx_client = httpx_client
        self._owns_httpx_client = httpx_client is None
        self._a2a_client: Optional[A2AClient] = None
        self._agent_card: Optional[AgentCard] = None
        # Track the context_id from the remote agent for session continuity
        self._last_context_id: Optional[str] = None

    async def close(self) -> None:
        """Close the underlying httpx client if this tool owns it.

        Should be called when the tool is no longer needed to prevent
        resource leaks.  Safe to call multiple times.
        """
        if self._owns_httpx_client and self._httpx_client is not None:
            try:
                await self._httpx_client.aclose()
                logger.debug("Closed httpx client for remote A2A tool %s", self.name)
            except Exception as e:
                logger.warning(
                    "Failed to close httpx client for remote A2A tool %s: %s",
                    self.name,
                    e,
                )
            finally:
                self._httpx_client = None
                self._a2a_client = None

    async def _ensure_client(self) -> A2AClient:
        """Lazily resolve the agent card and initialize the A2A client."""
        if self._a2a_client is not None:
            return self._a2a_client

        # Ensure we have an httpx client
        if self._httpx_client is None:
            self._httpx_client = httpx.AsyncClient(timeout=httpx.Timeout(timeout=600.0))
            self._owns_httpx_client = True

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

        # Create the A2A client
        config = A2AClientConfig(
            httpx_client=self._httpx_client,
            streaming=False,
            polling=False,
            supported_transports=[A2ATransport.jsonrpc],
        )
        factory = A2AClientFactory(config=config)
        self._a2a_client = factory.create(self._agent_card)
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

        task: Optional[Task] = None
        try:
            async for response in client.send_message(request=message):
                if isinstance(response, tuple):
                    # ClientEvent: (Task, UpdateEvent | None)
                    task = response[0]
                elif isinstance(response, A2AMessage):
                    # Direct message response (no task management)
                    if response.context_id:
                        self._last_context_id = response.context_id
                    return self._extract_text_from_message(response)
        except A2AClientHTTPError as e:
            return f"Remote agent '{self.name}' request failed: {e}"
        except Exception as e:
            logger.error("Error calling remote agent %s: %s", self.name, e, exc_info=True)
            return f"Remote agent '{self.name}' request failed: {e}"

        if task is None:
            return f"Remote agent '{self.name}' returned no result."

        # Track context_id for future requests to the same remote agent
        if task.context_id:
            self._last_context_id = task.context_id

        state = task.status.state if task.status else None

        if state == TaskState.input_required:
            return self._handle_input_required(task, tool_context)

        if state == TaskState.failed:
            error_text = _extract_text_from_task(task)
            return error_text or f"Remote agent '{self.name}' failed."

        # completed or any other terminal state
        return _extract_text_from_task(task) or ""

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

        logger.info(
            "DEBUG_ASKUSER _handle_resume: confirmed=%s, payload_keys=%s, payload=%s",
            confirmation.confirmed,
            list(payload.keys()) if payload else None,
            payload,
        )

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
            logger.info(
                "DEBUG_ASKUSER _handle_resume: forwarding ask_user_answers to subagent, answers=%s",
                ask_user_answers,
            )
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

        logger.info(
            "DEBUG_ASKUSER _handle_resume: sending decision_message to subagent %s, task_id=%s, decision_data=%s",
            subagent_name,
            task_id,
            decision_data,
        )

        client = await self._ensure_client()
        task: Optional[Task] = None
        try:
            async for response in client.send_message(request=decision_message):
                logger.info(
                    "DEBUG_ASKUSER _handle_resume: got response from subagent, type=%s, response=%s",
                    type(response).__name__,
                    response,
                )
                if isinstance(response, tuple):
                    task = response[0]
                elif isinstance(response, A2AMessage):
                    return self._extract_text_from_message(response)
        except A2AClientHTTPError as e:
            logger.error("DEBUG_ASKUSER _handle_resume: A2AClientHTTPError: %s", e, exc_info=True)
            return f"Remote agent '{subagent_name}' resume failed: {e}"
        except Exception as e:
            logger.error(
                "DEBUG_ASKUSER _handle_resume: Error resuming remote agent %s: %s", subagent_name, e, exc_info=True
            )
            return f"Remote agent '{subagent_name}' resume failed: {e}"

        if task is None:
            logger.warning("DEBUG_ASKUSER _handle_resume: task is None after resume")
            return f"Remote agent '{subagent_name}' returned no result after resume."

        state = task.status.state if task.status else None
        logger.info(
            "DEBUG_ASKUSER _handle_resume: subagent task state after resume: %s, task_id=%s",
            state,
            task.id,
        )

        if state == TaskState.input_required:
            # The subagent has another HITL request (e.g. multiple tools needing
            # approval in sequence). Surface it again.
            return self._handle_input_required(task, tool_context)

        if state == TaskState.failed:
            error_text = _extract_text_from_task(task)
            return error_text or f"Remote agent '{subagent_name}' failed after resume."

        return _extract_text_from_task(task) or ""

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
