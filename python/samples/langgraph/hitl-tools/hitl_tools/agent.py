"""LangGraph agent demonstrating HITL tool approval.

This sample builds a custom ReAct-style graph with two tools:
  - get_time: safe, runs without approval
  - delete_file: dangerous, requires human approval via interrupt()

The graph intercepts tool calls and checks whether they need approval.
If so, it calls interrupt() with the action_requests format that the
kagent LangGraph executor expects, pausing execution until the user
approves or rejects.

On resume, the executor passes the user's decision back via
Command(resume=...) and the graph reads it to proceed or skip.
"""

import logging
from datetime import datetime
from typing import Annotated, Any

import httpx
from kagent.core import KAgentConfig
from kagent.langgraph import KAgentCheckpointer
from langchain_core.messages import AIMessage, ToolMessage
from langchain_core.tools import tool
from langchain_openai import ChatOpenAI
from langgraph.graph import END, START, StateGraph
from langgraph.graph.message import add_messages
from langgraph.types import interrupt
from typing_extensions import TypedDict

logger = logging.getLogger(__name__)

kagent_checkpointer = KAgentCheckpointer(
    client=httpx.AsyncClient(base_url=KAgentConfig().url),
    app_name=KAgentConfig().app_name,
)

# -- Tools -------------------------------------------------------------------

# Tools that require human approval before execution.
TOOLS_REQUIRING_APPROVAL = {"delete_file"}


@tool
def get_time() -> str:
    """Get the current date and time. This is a safe tool that runs without approval."""
    return datetime.now().isoformat()


@tool
def delete_file(path: str) -> str:
    """Delete a file at the given path. This is a dangerous operation that requires human approval.

    Args:
        path: The file path to delete.
    """
    # In a real agent this would actually delete the file.
    # For this demo we just pretend.
    return f"File '{path}' has been deleted."


ALL_TOOLS = [get_time, delete_file]
TOOL_MAP = {t.name: t for t in ALL_TOOLS}

# -- Graph state -------------------------------------------------------------


class AgentState(TypedDict):
    messages: Annotated[list, add_messages]


# -- Graph nodes --------------------------------------------------------------

llm = ChatOpenAI(model="gpt-4o-mini").bind_tools(ALL_TOOLS)


async def call_model(state: AgentState) -> dict[str, Any]:
    """Call the LLM with the current messages."""
    response = await llm.ainvoke(state["messages"])
    return {"messages": [response]}


async def run_tools(state: AgentState) -> dict[str, Any]:
    """Execute tool calls, requesting approval for dangerous tools via interrupt().

    For each tool call in the last AI message:
      - If the tool is in TOOLS_REQUIRING_APPROVAL, call interrupt() with the
        action_requests format the kagent executor expects.  The executor
        converts this into an A2A input_required event so the frontend shows
        approve / reject buttons.
      - If approved (or no approval needed), execute the tool normally.
      - If rejected, return a message telling the LLM the tool was rejected.
    """
    last_message = state["messages"][-1]
    assert isinstance(last_message, AIMessage) and last_message.tool_calls

    results: list[ToolMessage] = []

    for tool_call in last_message.tool_calls:
        tool_name = tool_call["name"]
        tool_args = tool_call["args"]
        tool_call_id = tool_call["id"]

        if tool_name in TOOLS_REQUIRING_APPROVAL:
            # Pause execution and ask the user for approval.
            # The executor reads "action_requests" from the interrupt value
            # and emits an adk_request_confirmation DataPart to the frontend.
            decision = interrupt(
                {
                    "action_requests": [
                        {
                            "name": tool_name,
                            "args": tool_args,
                            "id": tool_call_id,
                        }
                    ]
                }
            )

            # The executor resumes with a dict like:
            #   {"decision_type": "approve"}
            #   {"decision_type": "reject", "rejection_reasons": {"*": "Too risky"}}
            decision_type = decision.get("decision_type", "reject") if isinstance(decision, dict) else "reject"

            if decision_type != "approve":
                reason = ""
                if isinstance(decision, dict):
                    reasons = decision.get("rejection_reasons", {})
                    reason = reasons.get("*", "") if isinstance(reasons, dict) else ""
                rejection_msg = "Tool call was rejected by user."
                if reason:
                    rejection_msg += f" Reason: {reason}"
                results.append(
                    ToolMessage(
                        content=rejection_msg,
                        tool_call_id=tool_call_id,
                        name=tool_name,
                    )
                )
                continue

        # Execute the tool (either no approval needed, or approved).
        tool_fn = TOOL_MAP[tool_name]
        result = await tool_fn.ainvoke(tool_args)
        results.append(
            ToolMessage(
                content=str(result),
                tool_call_id=tool_call_id,
                name=tool_name,
            )
        )

    return {"messages": results}


# -- Routing ------------------------------------------------------------------


def should_continue(state: AgentState) -> str:
    """Route to tools if the last message has tool calls, otherwise end."""
    last_message = state["messages"][-1]
    if isinstance(last_message, AIMessage) and last_message.tool_calls:
        return "tools"
    return END


# -- Build graph --------------------------------------------------------------

builder = StateGraph(AgentState)
builder.add_node("agent", call_model)
builder.add_node("tools", run_tools)

builder.add_edge(START, "agent")
builder.add_conditional_edges("agent", should_continue, {"tools": "tools", END: END})
builder.add_edge("tools", "agent")

graph = builder.compile(checkpointer=kagent_checkpointer)
