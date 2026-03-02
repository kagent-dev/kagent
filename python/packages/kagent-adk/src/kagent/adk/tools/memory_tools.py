"""Memory tools for agent skills."""

from __future__ import annotations

import json
import logging
from typing import Any, Dict, List, Optional

from google.adk.tools import BaseTool, ToolContext
from google.genai import types

logger = logging.getLogger("kagent_adk." + __name__)


def _strip_null_and_empty(obj: Any) -> Any:
    """Recursively remove None and empty dict/list to produce minimal JSON (saves tokens)."""
    if obj is None:
        return None
    if isinstance(obj, dict):
        out: Dict[str, Any] = {}
        for k, v in obj.items():
            v2 = _strip_null_and_empty(v)
            if v2 is not None and v2 != {} and v2 != []:
                out[k] = v2
        return out
    if isinstance(obj, list):
        out_list: List[Any] = []
        for item in obj:
            s = _strip_null_and_empty(item)
            if s is not None and s != {}:
                out_list.append(s)
        return out_list
    return obj


class SaveMemoryTool(BaseTool):
    """Tool to save specific information to long-term memory."""

    def __init__(self):
        """Initialize the SaveMemoryTool."""
        super().__init__(
            name="save_memory",
            description="Saves a specific piece of information or text to long-term memory. Use this to remember important facts, user preferences, or specific details for future reference.",
        )

    def _get_declaration(self) -> types.FunctionDeclaration:
        return types.FunctionDeclaration(
            name=self.name,
            description=self.description,
            parameters=types.Schema(
                type=types.Type.OBJECT,
                properties={
                    "content": types.Schema(
                        type=types.Type.STRING,
                        description="The text content or fact to save to memory.",
                    ),
                },
                required=["content"],
            ),
        )

    async def run_async(self, *, args: Dict[str, Any], tool_context: ToolContext) -> str:
        """Save content to memory."""
        try:
            # Access memory_service via protected member as it's not exposed publicly on ToolContext
            if not hasattr(tool_context, "_invocation_context") or not tool_context._invocation_context.memory_service:
                return "Error: Memory service is not available."

            content = args.get("content")
            if not content:
                return "Error: content is required."

            memory_service = tool_context._invocation_context.memory_service

            logger.info("Explicitly saving content to memory for session %s", tool_context.session.id)

            await memory_service.add_memory(
                app_name=tool_context.session.app_name,
                user_id=tool_context.session.user_id,
                content=content,
                metadata={"session_id": tool_context.session.id, "source": "explicit_save"},
            )
            return "Successfully saved information to long-term memory."

        except Exception as e:
            error_msg = f"Error saving memory: {e}"
            logger.error(error_msg)
            return error_msg


class LoadMemoryTool(BaseTool):
    """Tool to load memories from long-term memory."""

    def __init__(self):
        super().__init__(
            name="load_memory",
            description="Loads the memory for the current user. Returns a JSON string of memories.",
        )

    def _get_declaration(self) -> types.FunctionDeclaration:
        return types.FunctionDeclaration(
            name=self.name,
            description=self.description,
            parameters=types.Schema(
                type=types.Type.OBJECT,
                properties={
                    "query": types.Schema(
                        type=types.Type.STRING,
                        description="The query to search memory for.",
                    ),
                },
                required=["query"],
            ),
        )

    async def run_async(self, *, args: Dict[str, Any], tool_context: ToolContext) -> str:
        """Load memory for the current user."""
        try:
            query = args.get("query")
            if not query:
                return "Error: query is required."

            logger.info("Loading memory for query: %s", query)

            # Use helper method on ToolContext
            search_response = await tool_context.search_memory(query)

            # Serialize to minimal JSON (omit null/empty fields) to save tokens
            if hasattr(search_response, "model_dump"):
                compact = _strip_null_and_empty(search_response.model_dump())
                return json.dumps(compact)
            return str(search_response)

        except ValueError as e:
            return f"Error: {e}"
        except Exception as e:
            error_msg = f"Error loading memory: {e}"
            logger.error(error_msg)
            return error_msg
