import json
from unittest.mock import Mock

import pytest
from google.adk.tools.tool_confirmation import ToolConfirmation
from google.adk.tools.tool_context import ToolContext

from kagent.adk.tools.ask_user_tool import AskUserTool


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("args", "expected_error"),
    [
        ({}, "ask_user: at least one question is required"),
        (
            {"questions": "Which environment?"},
            "ask_user: questions must be a list of question objects",
        ),
        (
            {"questions": {"question": "Which environment?"}},
            "ask_user: questions must be a list of question objects",
        ),
        (
            {"questions": None},
            "ask_user: questions must be a list of question objects",
        ),
        (
            {"questions": ["Which environment?"]},
            "ask_user: question 1 must be an object",
        ),
        (
            {"questions": [None]},
            "ask_user: question 1 must be an object",
        ),
        (
            {"questions": [{"question": "Which environment?"}, "Which region?"]},
            "ask_user: question 2 must be an object",
        ),
        (
            {"questions": []},
            "ask_user: at least one question is required",
        ),
        ({"questions": [{}]}, "ask_user: question 1 must contain non-whitespace text"),
        ({"questions": [{"question": None}]}, "ask_user: question 1 must contain non-whitespace text"),
        ({"questions": [{"question": 42}]}, "ask_user: question 1 must contain non-whitespace text"),
        (
            {"questions": [{"question": ""}]},
            "ask_user: question 1 must contain non-whitespace text",
        ),
        (
            {"questions": [{"question": "   "}]},
            "ask_user: question 1 must contain non-whitespace text",
        ),
        (
            {"questions": [{"question": "\t\n"}]},
            "ask_user: question 1 must contain non-whitespace text",
        ),
        (
            {
                "questions": [
                    {"question": "Which environment?"},
                    {"question": " \t\n"},
                ]
            },
            "ask_user: question 2 must contain non-whitespace text",
        ),
    ],
)
async def test_rejects_invalid_questions_without_requesting_confirmation(args, expected_error):
    context = Mock(spec=ToolContext)
    context.tool_confirmation = None

    result = await AskUserTool().run_async(args=args, tool_context=context)

    assert result == expected_error
    context.request_confirmation.assert_not_called()


@pytest.mark.asyncio
async def test_cancelled_question_returns_cancelled_without_new_confirmation():
    context = Mock(spec=ToolContext)
    context.tool_confirmation = ToolConfirmation(confirmed=False)

    result = await AskUserTool().run_async(
        args={"questions": [{"question": "Which environment?"}]}, tool_context=context
    )

    assert json.loads(result) == {"status": "cancelled"}
    context.request_confirmation.assert_not_called()


@pytest.mark.asyncio
async def test_valid_question_requests_confirmation_without_rewriting_text():
    context = Mock(spec=ToolContext)
    context.tool_confirmation = None
    questions = [
        {
            "question": "  Which environment?  ",
            "choices": ["prod", "staging"],
            "multiple": True,
        }
    ]

    result = await AskUserTool().run_async(
        args={"questions": questions},
        tool_context=context,
    )

    assert result == {"status": "pending", "questions": questions}
    context.request_confirmation.assert_called_once_with(hint="  Which environment?  ")


@pytest.mark.asyncio
async def test_valid_confirmed_question_returns_answer_without_new_confirmation():
    context = Mock(spec=ToolContext)
    context.tool_confirmation = ToolConfirmation(
        confirmed=True,
        payload={"answers": [{"answer": "prod"}]},
    )

    result = await AskUserTool().run_async(
        args={"questions": [{"question": "  Which environment?  "}]},
        tool_context=context,
    )

    assert json.loads(result) == [
        {
            "question": "  Which environment?  ",
            "answer": "prod",
        }
    ]
    context.request_confirmation.assert_not_called()
