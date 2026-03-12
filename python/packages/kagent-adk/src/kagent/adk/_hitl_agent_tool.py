"""AgentTool subclass that propagates sub-agent HITL via ToolContext.request_confirmation().

When the wrapped RemoteA2aAgent enters input_required (e.g., from a tool requiring
human approval or an ask_user call), this tool pauses execution using the ADK's native
ToolContext HITL mechanism. On resume, the user's decision is forwarded to the
sub-agent via its A2A client.

This keeps HITL logic in the tool layer (not the executor) and uses ADK's native
confirmation mechanism, consistent with the design established in PR #1398.
"""

from __future__ import annotations

import asyncio
import logging
import uuid
from typing import Any

from a2a.client.middleware import ClientCallContext
from a2a.types import (
    DataPart as A2ADataPart,
    Message as A2AMessage,
    Part as A2APart,
    Role,
    TaskState,
    TextPart as A2ATextPart,
)
from google.adk.tools.agent_tool import AgentTool, _get_input_schema, _get_output_schema
from google.adk.tools.tool_context import ToolContext
from google.adk.utils.context_utils import Aclosing
from google.genai import types as genai_types
from typing_extensions import override

from kagent.core.a2a import (
    KAGENT_HITL_DECISION_TYPE_APPROVE,
    KAGENT_HITL_DECISION_TYPE_KEY,
    KAGENT_HITL_DECISION_TYPE_REJECT,
)

logger = logging.getLogger(__name__)


class HitlAwareAgentTool(AgentTool):
    """AgentTool that uses ToolContext.request_confirmation() for sub-agent HITL.

    When the wrapped agent enters input_required, this tool:
    1. Saves the sub-agent's task/context IDs in tool_context.state
    2. Calls tool_context.request_confirmation() to pause execution
    3. On resume, forwards the user's decision to the sub-agent via A2A

    This keeps HITL logic in the tool layer (not the executor) and uses
    ADK's native confirmation mechanism, consistent with the design
    established in PR #1398.
    """

    # State key for persisting sub-agent HITL context across tool replays
    _SUBAGENT_HITL_STATE_KEY = "_subagent_hitl"

    # Timeout for forwarding decisions to sub-agents
    _FORWARD_TIMEOUT_SECONDS = 300

    @override
    async def run_async(
        self,
        *,
        args: dict[str, Any],
        tool_context: ToolContext,
    ) -> Any:
        # Resume path: user has responded to a HITL confirmation
        if tool_context.tool_confirmation is not None:
            if not tool_context.tool_confirmation.confirmed:
                return self._handle_rejection(tool_context)
            return await self._forward_and_continue(tool_context)

        # First invocation: run the sub-agent with HITL detection
        return await self._run_with_hitl_detection(args, tool_context)

    # ------------------------------------------------------------------
    # First invocation — run sub-agent and detect input_required
    # ------------------------------------------------------------------

    async def _run_with_hitl_detection(
        self,
        args: dict[str, Any],
        tool_context: ToolContext,
    ) -> Any:
        """Run the sub-agent, pausing via request_confirmation if it enters input_required.

        Mirrors AgentTool.run_async() but adds input_required detection in the
        event loop so we can surface the sub-agent's HITL request to the parent.
        """
        from google.adk.memory.in_memory_memory_service import InMemoryMemoryService
        from google.adk.runners import Runner
        from google.adk.sessions.in_memory_session_service import InMemorySessionService
        from google.adk.tools._forwarding_artifact_service import ForwardingArtifactService

        if self.skip_summarization:
            tool_context.actions.skip_summarization = True

        content = self._build_input_content(args)

        invocation_context = tool_context._invocation_context
        parent_app_name = invocation_context.app_name if invocation_context else None
        child_app_name = parent_app_name or self.agent.name
        plugins = invocation_context.plugin_manager.plugins if self.include_plugins else None

        runner = Runner(
            app_name=child_app_name,
            agent=self.agent,
            artifact_service=ForwardingArtifactService(tool_context),
            session_service=InMemorySessionService(),
            memory_service=InMemoryMemoryService(),
            credential_service=invocation_context.credential_service,
            plugins=plugins,
        )

        state_dict = {k: v for k, v in tool_context.state.to_dict().items() if not k.startswith("_adk")}
        session = await runner.session_service.create_session(
            app_name=child_app_name,
            user_id=invocation_context.user_id,
            state=state_dict,
        )

        last_content = None
        try:
            async with Aclosing(
                runner.run_async(
                    user_id=session.user_id,
                    session_id=session.id,
                    new_message=content,
                )
            ) as event_stream:
                async for event in event_stream:
                    if event.actions.state_delta:
                        tool_context.state.update(event.actions.state_delta)
                    if event.content:
                        last_content = event.content

                    # Detect sub-agent entering input_required
                    a2a_response = _extract_input_required(event)
                    if a2a_response is not None:
                        _save_hitl_state(tool_context, a2a_response)
                        hint = _extract_hitl_hint(a2a_response)
                        tool_context.request_confirmation(hint=hint)
                        return {"status": "confirmation_requested", "subagent_hitl": True}
        finally:
            await runner.close()

        return self._format_result(last_content)

    # ------------------------------------------------------------------
    # Resume path — forward decision and collect result
    # ------------------------------------------------------------------

    def _handle_rejection(self, tool_context: ToolContext) -> str:
        """Handle a rejected HITL confirmation."""
        tool_context.state.pop(self._SUBAGENT_HITL_STATE_KEY, None)
        payload = tool_context.tool_confirmation.payload or {}
        reason = payload.get("rejection_reason", "") if isinstance(payload, dict) else ""
        if reason:
            return f"Sub-agent action was rejected by user. Reason: {reason}"
        return "Sub-agent action was rejected by user."

    async def _forward_and_continue(self, tool_context: ToolContext) -> Any:
        """Forward the user's approval to the sub-agent and return the result."""
        hitl_state = tool_context.state.get(self._SUBAGENT_HITL_STATE_KEY)
        if not hitl_state:
            logger.error("No sub-agent HITL state found for resume")
            return {"error": "No sub-agent HITL state found for resume"}

        task_id = hitl_state.get("task_id")
        context_id = hitl_state.get("context_id")

        # Ensure the remote agent is resolved (prefer public API, fall back to private)
        ensure_resolved = getattr(self.agent, "ensure_resolved", None) or getattr(self.agent, "_ensure_resolved", None)
        if ensure_resolved is None:
            logger.error("RemoteA2aAgent has no ensure_resolved method")
            tool_context.state.pop(self._SUBAGENT_HITL_STATE_KEY, None)
            return {"error": "Cannot connect to sub-agent: missing ensure_resolved method"}

        try:
            await ensure_resolved()
        except Exception as exception:
            logger.error("Failed to resolve RemoteA2aAgent: %s", exception)
            tool_context.state.pop(self._SUBAGENT_HITL_STATE_KEY, None)
            return {"error": f"Failed to connect to sub-agent: {exception}"}

        # Get the A2A client (private API — no public send/resume API exists yet)
        a2a_client = getattr(self.agent, "_a2a_client", None)
        send_message = getattr(a2a_client, "send_message", None) if a2a_client else None
        if send_message is None:
            logger.error("RemoteA2aAgent does not expose A2A send_message")
            tool_context.state.pop(self._SUBAGENT_HITL_STATE_KEY, None)
            return {
                "error": "Cannot communicate with sub-agent: "
                "RemoteA2aAgent does not expose '_a2a_client.send_message'. "
                "Upgrade google-adk to a version with a public send/resume API."
            }

        # Build the decision message to forward
        decision_message = A2AMessage(
            message_id=str(uuid.uuid4()),
            role=Role.user,
            parts=[A2APart(A2ADataPart(data={KAGENT_HITL_DECISION_TYPE_KEY: KAGENT_HITL_DECISION_TYPE_APPROVE}))],
            task_id=task_id,
            context_id=context_id,
        )

        # Forward with timeout
        try:
            result_text, needs_more_input, new_a2a_response = await asyncio.wait_for(
                _send_and_collect(send_message, decision_message, tool_context),
                timeout=self._FORWARD_TIMEOUT_SECONDS,
            )
        except asyncio.TimeoutError:
            logger.error(
                "Timed out forwarding decision to sub-agent after %ds",
                self._FORWARD_TIMEOUT_SECONDS,
            )
            tool_context.state.pop(self._SUBAGENT_HITL_STATE_KEY, None)
            return {"error": f"Timed out waiting for sub-agent to respond (>{self._FORWARD_TIMEOUT_SECONDS}s)"}
        except Exception as exception:
            logger.error("Failed to forward decision to sub-agent: %s", exception, exc_info=True)
            tool_context.state.pop(self._SUBAGENT_HITL_STATE_KEY, None)
            return {"error": f"Failed to communicate with sub-agent: {exception}"}

        # If the sub-agent needs another round of input, re-request confirmation
        if needs_more_input and new_a2a_response is not None:
            _save_hitl_state(tool_context, new_a2a_response)
            hint = _extract_hitl_hint(new_a2a_response)
            tool_context.request_confirmation(hint=hint)
            return {"status": "confirmation_requested", "subagent_hitl": True}

        # Clear HITL state after successful completion
        tool_context.state.pop(self._SUBAGENT_HITL_STATE_KEY, None)
        return result_text or ""

    # ------------------------------------------------------------------
    # Helpers
    # ------------------------------------------------------------------

    def _build_input_content(self, args: dict[str, Any]) -> genai_types.Content:
        """Build the genai Content to send to the sub-agent (mirrors AgentTool logic)."""
        input_schema = _get_input_schema(self.agent)
        if input_schema:
            input_value = input_schema.model_validate(args)
            return genai_types.Content(
                role="user",
                parts=[genai_types.Part.from_text(text=input_value.model_dump_json(exclude_none=True))],
            )
        return genai_types.Content(
            role="user",
            parts=[genai_types.Part.from_text(text=args["request"])],
        )

    def _format_result(self, last_content: genai_types.Content | None) -> Any:
        """Format the sub-agent's last content into a return value (mirrors AgentTool logic)."""
        if last_content is None or last_content.parts is None:
            return ""
        merged_text = "\n".join(p.text for p in last_content.parts if p.text and not p.thought)
        output_schema = _get_output_schema(self.agent)
        if output_schema:
            return output_schema.model_validate_json(merged_text).model_dump(exclude_none=True)
        return merged_text


# ------------------------------------------------------------------
# Module-level helpers (stateless, testable independently)
# ------------------------------------------------------------------


def _extract_input_required(event: Any) -> dict | None:
    """Check if an ADK event indicates the remote agent entered input_required.

    Returns the A2A response dict if input_required was detected, None otherwise.
    """
    custom_metadata = getattr(event, "custom_metadata", None)
    if not custom_metadata:
        return None

    a2a_response = custom_metadata.get("a2a:response")
    if not isinstance(a2a_response, dict):
        return None

    status = a2a_response.get("status", {})
    if status.get("state") == "input-required":
        return a2a_response

    return None


def _extract_hitl_hint(a2a_response: dict) -> str:
    """Extract a human-readable HITL hint from the A2A response's status message."""
    status = a2a_response.get("status", {})
    message = status.get("message", {})
    parts = message.get("parts", [])
    for part in parts:
        if isinstance(part, dict):
            text = part.get("text")
            if text:
                return text
    return "Sub-agent requires user input."


def _save_hitl_state(tool_context: ToolContext, a2a_response: dict) -> None:
    """Persist sub-agent HITL context in tool_context.state for resume."""
    tool_context.state[HitlAwareAgentTool._SUBAGENT_HITL_STATE_KEY] = {
        "task_id": a2a_response.get("id"),
        "context_id": a2a_response.get("contextId") or a2a_response.get("context_id"),
    }


async def _send_and_collect(
    send_message: Any,
    message: A2AMessage,
    tool_context: ToolContext,
) -> tuple[str, bool, dict | None]:
    """Send a message to the sub-agent and collect the response.

    Returns (result_text, needs_more_input, a2a_response_dict_for_next_hitl).
    """
    result_parts: list[str] = []
    needs_input = False
    a2a_response_dict: dict | None = None

    state_dict = tool_context.state.to_dict() if hasattr(tool_context.state, "to_dict") else dict(tool_context.state)

    async for a2a_response in send_message(
        request=message,
        context=ClientCallContext(state=state_dict),
    ):
        if isinstance(a2a_response, tuple):
            task, update = a2a_response

            if update is None and task and task.status:
                # Initial task response (non-streaming or first response)
                if task.status.state == TaskState.completed:
                    _collect_text_parts(task.status.message, result_parts)
                elif task.status.state == TaskState.failed:
                    return _extract_error_text(task.status.message), False, None
                elif task.status.state == TaskState.input_required:
                    needs_input = True
                    a2a_response_dict = task.model_dump(exclude_none=True, by_alias=True)

            elif update is not None and hasattr(update, "status") and update.status:
                # Streaming status update
                if update.status.state == TaskState.completed:
                    _collect_text_parts(update.status.message, result_parts)
                elif update.status.state == TaskState.failed:
                    return _extract_error_text(update.status.message), False, None
                elif update.status.state == TaskState.input_required:
                    needs_input = True
                    if task:
                        a2a_response_dict = task.model_dump(exclude_none=True, by_alias=True)
        else:
            # Non-streaming message response
            if hasattr(a2a_response, "parts") and a2a_response.parts:
                _collect_text_parts(a2a_response, result_parts)

    return "\n".join(result_parts), needs_input, a2a_response_dict


def _collect_text_parts(message: Any, result_parts: list[str]) -> None:
    """Extract text from an A2A message's parts and append to result_parts."""
    if not message or not getattr(message, "parts", None):
        return
    for part in message.parts:
        root = part.root if hasattr(part, "root") else part
        if isinstance(root, A2ATextPart):
            result_parts.append(root.text)


def _extract_error_text(message: Any) -> str:
    """Extract error text from a failed A2A message."""
    if message and getattr(message, "parts", None):
        for part in message.parts:
            root = part.root if hasattr(part, "root") else part
            if isinstance(root, A2ATextPart):
                return root.text
    return "Sub-agent execution failed"
