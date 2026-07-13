"""MCP App (UI) tool result compaction for the model.

Mirrors the Go ADK behavior in ``go/adk/pkg/agent/mcp_apps.go``. An MCP App tool
(one declaring a ``ui.resourceUri``) renders an interactive widget in the chat
that updates itself in place. The model must treat a successful render as a
completed, self-updating artifact; otherwise it tends to re-invoke the rendering
tool on every "refresh", flooding the chat with duplicate app cards (observed
with weaker models calling the same render tool 5-7 times in a row).

We keep the full tool payload in the session/chat history so the UI can render
the widget, and only rewrite what the *model* sees into a short terminal
directive. ADK builds the model request from deep copies of the session events
(see ``flows/llm_flows/contents.py``), so mutating the request here does not
corrupt the stored history.
"""

from __future__ import annotations

from google.adk.agents.callback_context import CallbackContext
from google.adk.models.llm_request import LlmRequest

# Terminal directive the model sees in place of an MCP App tool's render
# payload. It is protocol-oriented: it applies to any tool carrying a UI
# resourceUri, independent of the tool's name or payload keys.
MCP_APP_RENDERED_NOTICE = (
    "The interactive UI for this tool has been rendered to the user and now "
    "updates live inside the app. Treat this as complete and do not call this "
    "tool again unless the user explicitly asks for it."
)


class MCPAppToolNames:
    """Mutable, shared set of MCP App (UI-rendering) tool names.

    Populated lazily by ``KAgentMcpToolset.get_tools`` as MCP tools are resolved
    (which happens during request preprocessing, before ``before_model_callback``
    runs) and read by the compaction callback. Using a shared object avoids
    re-listing MCP tools on every model turn.
    """

    def __init__(self) -> None:
        self._names: set[str] = set()

    def add(self, name: str) -> None:
        self._names.add(name)

    def __contains__(self, name: str) -> bool:
        return name in self._names

    def __bool__(self) -> bool:
        return bool(self._names)

    def __len__(self) -> int:
        return len(self._names)


def compact_mcp_app_response(response: dict) -> dict:
    """Rewrite an MCP App tool result (a JSON ``CallToolResult``) for the model.

    On error, keep the content so the model can diagnose/recover but drop the
    heavy structured payload. On success, collapse the render payload into a
    terminal directive so the model stops re-invoking the rendering tool,
    preserving ``_meta`` (e.g. resourceUri) for any downstream tooling.
    """
    if response.get("isError") is True or response.get("error") is True:
        compacted = dict(response)
        compacted.pop("structuredContent", None)
        return compacted

    compacted: dict = {"content": [{"type": "text", "text": MCP_APP_RENDERED_NOTICE}]}
    if "_meta" in response:
        compacted["_meta"] = response["_meta"]
    return compacted


def make_mcp_app_model_result_callback(app_tool_names: MCPAppToolNames):
    """Build a ``before_model_callback`` that compacts MCP App tool results.

    Only the model's view is changed; the full result remains in chat history
    for UI rendering. The callback is a no-op until ``app_tool_names`` has been
    populated, so it is safe to attach unconditionally.
    """

    def before_model(callback_context: CallbackContext, llm_request: LlmRequest) -> None:
        if not app_tool_names or not llm_request.contents:
            return None
        for content in llm_request.contents:
            for part in content.parts or []:
                function_response = part.function_response
                if function_response is None or function_response.name not in app_tool_names:
                    continue
                if isinstance(function_response.response, dict):
                    function_response.response = compact_mcp_app_response(function_response.response)
        return None

    return before_model
