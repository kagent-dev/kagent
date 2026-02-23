# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Plugin that captures A2A sub-agent session metadata.

When a parent agent delegates to a remote sub-agent via AgentTool, this plugin:
1. Detects the sub-agent's context_id from A2A event metadata (on_event_callback)
2. Embeds the context_id in the tool result for historical/stored access (after_tool_callback)

This plugin is automatically propagated to child runners via AgentTool's
include_plugins=True default, so on_event_callback fires on the child runner's
events where A2A metadata is present.
"""

from __future__ import annotations

import contextvars
import logging
from dataclasses import dataclass
from typing import Any, Optional, TYPE_CHECKING

from google.adk.plugins.base_plugin import BasePlugin
from google.adk.tools.agent_tool import AgentTool

if TYPE_CHECKING:
    from google.adk.agents.invocation_context import InvocationContext
    from google.adk.events.event import Event
    from google.adk.tools.base_tool import BaseTool
    from google.adk.tools.tool_context import ToolContext

logger = logging.getLogger("kagent_adk." + __name__)


@dataclass
class _ToolCallState:
    agent_tool_name: str | None = None
    function_call_id: str | None = None
    captured_context_id: str | None = None
    captured_task_id: str | None = None


_current_tool_call: contextvars.ContextVar[_ToolCallState | None] = contextvars.ContextVar(
    "_current_tool_call", default=None
)


def get_current_function_call_id() -> str | None:
    """Return the function_call_id of the currently executing AgentTool call.

    This is used by the a2a_request_meta_provider to inject the parent's
    function_call_id into outgoing A2A requests so the sub-agent can store it.
    """
    tc = _current_tool_call.get(None)
    return tc.function_call_id if tc else None


class SubAgentSessionPlugin(BasePlugin):
    """Captures A2A sub-agent session context_id and embeds it in tool results."""

    def __init__(self):
        super().__init__(name="sub_agent_session")

    async def before_tool_callback(
        self,
        *,
        tool: BaseTool,
        tool_args: dict[str, Any],
        tool_context: ToolContext,
    ) -> Optional[dict]:
        if isinstance(tool, AgentTool):
            _current_tool_call.set(
                _ToolCallState(
                    agent_tool_name=tool.agent.name if hasattr(tool, "agent") else tool.name,
                    function_call_id=tool_context.function_call_id,
                )
            )
        return None

    async def on_event_callback(
        self,
        *,
        invocation_context: InvocationContext,
        event: Event,
    ) -> Optional[Event]:
        if not event.custom_metadata:
            return None

        tc = _current_tool_call.get(None)
        if tc is None:
            return None

        context_id = event.custom_metadata.get("a2a:context_id")
        task_id = event.custom_metadata.get("a2a:task_id")

        if not context_id and not task_id:
            return None

        if context_id and not tc.captured_context_id:
            tc.captured_context_id = context_id
        if task_id and not tc.captured_task_id:
            tc.captured_task_id = task_id

        return None

    async def after_tool_callback(
        self,
        *,
        tool: BaseTool,
        tool_args: dict[str, Any],
        tool_context: ToolContext,
        result: dict,
    ) -> Optional[dict]:
        if not isinstance(tool, AgentTool):
            return None

        tc = _current_tool_call.get(None)
        if tc is None or (not tc.captured_context_id and not tc.captured_task_id):
            return None

        if isinstance(result, str):
            result = {"result": result}

        if isinstance(result, dict):
            if tc.captured_context_id:
                result["a2a:context_id"] = tc.captured_context_id
            if tc.captured_task_id:
                result["a2a:task_id"] = tc.captured_task_id
            return result

        return None
