"""Built-in tool for asking the user questions during agent execution.

Uses the ADK-native ToolContext.request_confirmation() mechanism so that
the standard HITL event plumbing (adk_request_confirmation, long_running_tool_ids,
executor resume path) handles the interrupt and resume transparently.
"""

from __future__ import annotations

import json
import logging
from typing import Any

from google.adk.tools.base_tool import BaseTool
from google.adk.tools.tool_context import ToolContext
from google.genai import types

logger = logging.getLogger(__name__)

# Schema for a single question object passed to ask_user.
_QUESTION_SCHEMA = types.Schema(
    type=types.Type.OBJECT,
    properties={
        "question": types.Schema(
            type=types.Type.STRING,
            description="The question text to display to the user.",
        ),
        "choices": types.Schema(
            type=types.Type.ARRAY,
            items=types.Schema(type=types.Type.STRING),
            description=(
                "Predefined answer choices shown as selectable chips. Leave empty for a free-text-only question."
            ),
        ),
        "multiple": types.Schema(
            type=types.Type.BOOLEAN,
            description=("If true, the user can select multiple choices. Defaults to false (single-select)."),
        ),
    },
    required=["question"],
)


class AskUserTool(BaseTool):
    """Built-in tool that lets the agent ask the user one or more questions.

    The tool uses the ADK ``request_confirmation`` mechanism to pause
    execution and present the questions to the UI.  On the resume path the
    UI sends back ``ask_user_answers`` in the approval DataPart and the
    executor injects them via ``ToolConfirmation.payload``.

    Because the interrupt is driven by ``request_confirmation``, this tool
    does *not* need to be listed in ``tools_requiring_approval``.
    """

    def __init__(self) -> None:
        super().__init__(
            name="ask_user",
            description=(
                "Ask the user one or more questions and wait for their answers "
                "before continuing. Use this when you need clarifying information, "
                "preferences, or explicit confirmation from the user."
            ),
        )

    def _get_declaration(self) -> types.FunctionDeclaration:
        return types.FunctionDeclaration(
            name=self.name,
            description=self.description,
            parameters=types.Schema(
                type=types.Type.OBJECT,
                properties={
                    "questions": types.Schema(
                        type=types.Type.ARRAY,
                        items=_QUESTION_SCHEMA,
                        description="List of questions to ask the user.",
                    ),
                },
                required=["questions"],
            ),
        )

    async def run_async(
        self,
        *,
        args: dict[str, Any],
        tool_context: ToolContext,
    ) -> Any:
        questions: list[dict] = args.get("questions", [])

        if tool_context.tool_confirmation is None:
            # First invocation — pause execution and ask the user.
            summary = "; ".join(q.get("question", "") for q in questions if q.get("question"))
            tool_context.request_confirmation(hint=summary or "Questions for the user.")
            logger.debug("ask_user: requesting confirmation with %d question(s)", len(questions))
            return {"status": "pending", "questions": questions}

        if tool_context.tool_confirmation.confirmed:
            # Second invocation — the executor injected answers via payload.
            payload = tool_context.tool_confirmation.payload or {}
            answers: list[dict] = payload.get("answers", []) if isinstance(payload, dict) else []
            result = []
            for i, q in enumerate(questions):
                ans = answers[i]["answer"] if i < len(answers) and "answer" in answers[i] else []
                result.append({"question": q.get("question", ""), "answer": ans})
            logger.debug("ask_user: returning %d answer(s)", len(result))
            return json.dumps(result)

        # User cancelled or rejected (should not normally happen for ask_user).
        logger.debug("ask_user: confirmation not received, returning cancelled status")
        return json.dumps({"status": "cancelled"})
