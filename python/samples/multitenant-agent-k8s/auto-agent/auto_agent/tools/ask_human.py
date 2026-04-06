"""AskHumanTool — Human-in-the-Loop via LangGraph interrupt.

Pauses the graph and waits for human input.
Use before ANY create / update / delete action.

Flow:
1. Agent calls ask_human(question, options)
2. interrupt() fires with kagent action_requests payload
3. kagent executor emits input_required + adk_request_confirmation DataPart
4. User approves/rejects via TG Bot or kagent UI
5. Graph resumes with the answer

Interrupt payload:
  {"action_requests": [{"name": "ask_human", "args": {question, options}, "id": "<uuid>"}]}

Resume value:
  {"decision_type": "approve"|"reject", "ask_user_answers": [{"answer": ["<text>"]}]}
"""
from __future__ import annotations

import uuid

from langchain_core.tools import tool
from langgraph.types import interrupt


@tool
def ask_human(question: str, options: list[str] | None = None) -> str:
    """Ask the user a question and wait for their answer before continuing.

    ALWAYS call this before any action that creates, updates, or deletes data.

    Args:
        question: what you are about to do and why
        options:  suggested responses (e.g. ["Да", "Нет"]). None = free-form.

    Returns:
        The human's answer, or "rejected" if the user denied.
    """
    # kagent executor requires action_requests format to emit input_required.
    payload = {
        "action_requests": [
            {
                "name": "ask_human",
                "args": {"question": question, "options": options or []},
                "id": str(uuid.uuid4()),
            }
        ]
    }

    resume_value = interrupt(payload)

    if isinstance(resume_value, dict):
        decision = resume_value.get("decision_type", "approve")

        if decision == "reject":
            reasons = resume_value.get("rejection_reasons", {})
            reason = reasons.get("*", "") if isinstance(reasons, dict) else ""
            return f"rejected: {reason}" if reason else "rejected"

        # approve — extract text from ask_user_answers[0]["answer"][0]
        answers = resume_value.get("ask_user_answers", [])
        if answers and isinstance(answers, list):
            first = answers[0]
            if isinstance(first, dict):
                answer_list = first.get("answer", [])
                if answer_list and isinstance(answer_list, list):
                    return str(answer_list[0])

    # resume_value is a plain string (e.g. in tests)
    return str(resume_value)
