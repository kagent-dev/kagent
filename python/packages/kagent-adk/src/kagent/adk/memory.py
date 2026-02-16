import logging
import json
from typing import Any, Optional

from google.adk.memory import BaseMemoryService
from google.adk.memory.base_memory_service import SearchMemoryResponse
from google.adk.sessions import Session
from google.adk.tools.mcp_tool import SseConnectionParams, StreamableHTTPConnectionParams
from kagent.adk._mcp_toolset import KAgentMcpToolset

logger = logging.getLogger(__name__)


class McpMemoryService(BaseMemoryService):
    """Memory service that delegates to an MCP server."""

    def __init__(self, connection_params: SseConnectionParams | StreamableHTTPConnectionParams):
        super().__init__()
        # Use KAgentMcpToolset for consistent error handling and features
        self.toolset = KAgentMcpToolset(connection_params=connection_params)
        self._tools = None

    async def _ensure_tools(self):
        if self._tools is None:
            tools = await self.toolset.get_tools()
            self._tools = {t.name: t for t in tools}

    async def _call_tool(self, name: str, **kwargs) -> Any:
        await self._ensure_tools()
        if name not in self._tools:
            # Try to find a tool that matches the name loosely or assume the server provides it?
            # For now, strict match.
            logger.warning(f"Tool '{name}' not found in MCP server. Available tools: {list(self._tools.keys())}")
            raise ValueError(f"Tool '{name}' not found in MCP server.")
        
        tool = self._tools[name]
        logger.debug(f"Calling memory tool '{name}' with kwargs: {kwargs.keys()}")
        return await tool.run(**kwargs)

    async def add_session_to_memory(self, session: Session) -> None:
        """Adds a session to the memory store."""
        try:
            # We pass the session as a dict
            # session.model_dump(mode='json') creates a dict with JSON-compatible types
            session_data = session.model_dump(mode="json")
            await self._call_tool("add_session_to_memory", session=session_data)
        except Exception as e:
            logger.error(f"Failed to add session to memory: {e}")
            raise

    async def search_memory(self, *, app_name: str, user_id: str, query: str) -> SearchMemoryResponse:
        """Searches the memory store."""
        try:
            result = await self._call_tool("search_memory", app_name=app_name, user_id=user_id, query=query)
            
            # The result should be compatible with SearchMemoryResponse
            if isinstance(result, dict):
                 return SearchMemoryResponse.model_validate(result)
            elif isinstance(result, str):
                 try:
                     # Try to parse json if string
                     data = json.loads(result)
                     if isinstance(data, dict):
                         return SearchMemoryResponse.model_validate(data)
                 except json.JSONDecodeError:
                     pass
                 
                 # Maybe the string itself is content? Unlikely for SearchMemoryResponse.
            
            # If result is an object (like a Pydantic model from ADK tools?), try model_dump
            if hasattr(result, "model_dump"):
                return SearchMemoryResponse.model_validate(result.model_dump())

            raise ValueError(f"Unexpected result type from search_memory: {type(result)}")
        except Exception as e:
            logger.error(f"Failed to search memory: {e}")
            raise
