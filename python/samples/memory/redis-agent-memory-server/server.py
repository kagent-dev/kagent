import asyncio
import logging
import os
from datetime import datetime
from typing import Any, Dict, List, Optional

import uvicorn
from agent_memory_client import MemoryAPIClient
from dotenv import load_dotenv
from mcp.server.fastmcp import FastMCP
from mcp.server.sse import SseServerTransport
from starlette.applications import Starlette
from starlette.routing import Mount, Route

# Load environment variables
load_dotenv()

# Configuration
MEMORY_API_URL = os.getenv("MEMORY_API_URL", "http://localhost:8000")
MEMORY_API_KEY = os.getenv("MEMORY_API_KEY", None)
MCP_PORT = int(os.getenv("MCP_PORT", "8000"))
MCP_TRANSPORT = os.getenv("MCP_TRANSPORT", "stdio")  # 'stdio' or 'sse'

# Initialize Client
client = MemoryAPIClient(base_url=MEMORY_API_URL, api_key=MEMORY_API_KEY)

# Initialize MCP Server
mcp = FastMCP("Redis Agent Memory Client")

logger = logging.getLogger(__name__)
logging.basicConfig(level=logging.INFO)

# --- McpMemoryService Compatibility Tools ---


@mcp.tool()
def add_session_to_memory(session: Dict[str, Any]) -> Dict[str, Any]:
    """
    Adds a session to the memory store.
    Required by kagent-adk McpMemoryService.
    """
    session_id = session.get("id")
    user_id = session.get("user_id")
    namespace = session.get("app_name")
    events = session.get("events", [])

    messages = []
    for event in events:
        content = event.get("content")
        author = event.get("author", "user")  # Default to user

        # Redis Agent Memory Server expects 'role' and 'content'
        role = "user"
        if isinstance(author, str):
            if author.lower() in ("model", "assistant", "bot", "ai"):
                role = "assistant"
            elif author.lower() == "system":
                role = "system"

        if content:
            messages.append(
                {"role": role, "content": content, "id": event.get("id"), "created_at": event.get("timestamp")}
            )

    logger.info(f"Adding session {session_id} to memory with {len(messages)} messages")

    return client.set_working_memory(session_id=session_id, messages=messages, user_id=user_id, namespace=namespace)


@mcp.tool()
def search_memory(app_name: str, user_id: str, query: str) -> Dict[str, Any]:
    """
    Searches the memory store.
    Required by kagent-adk McpMemoryService.
    Returns: {"memories": [{"content": "...", ...}, ...]}
    """
    logger.info(f"Searching memory for user {user_id} in {app_name}: {query}")

    results = client.search_long_term_memory(text=query, user_id=user_id, namespace=app_name, limit=5)

    raw_memories = results.get("memories", [])
    formatted_memories = []

    for m in raw_memories:
        # Construct content structure compatible with google.genai.types.Content
        content_text = m.get("text", "")
        formatted_memories.append(
            {
                "content": {"parts": [{"text": content_text}], "role": "user"},
                "id": m.get("id"),
                "author": m.get("user_id"),
                "timestamp": m.get("created_at"),
                "custom_metadata": {
                    "topics": m.get("topics"),
                    "entities": m.get("entities"),
                    "memory_type": m.get("memory_type"),
                },
            }
        )

    return {"memories": formatted_memories}


# --- General Redis Agent Memory Server Tools ---


@mcp.tool()
def get_current_datetime() -> dict[str, str | int]:
    """
    Get the current datetime in UTC for grounding relative time expressions.
    """
    now = datetime.utcnow()
    iso_utc = now.replace(microsecond=0).isoformat() + "Z"
    return {"iso_utc": iso_utc, "unix_ts": int(now.timestamp())}


@mcp.tool()
def create_long_term_memories(memories: List[Dict[str, Any]]) -> Dict[str, Any]:
    """
    Create long-term memories directly.
    """
    return client.create_long_term_memories(memories=memories)


@mcp.tool()
def memory_prompt(
    query: str,
    session_id: str | None = None,
    namespace: str | None = None,
    user_id: str | None = None,
    limit: int = 10,
) -> Dict[str, Any]:
    """
    Hydrate a query with relevant context.
    """
    return client.memory_prompt(query=query, session_id=session_id, namespace=namespace, user_id=user_id, limit=limit)


@mcp.tool()
def delete_long_term_memories(memory_ids: List[str]) -> Dict[str, Any]:
    """Delete memories by ID."""
    return client.delete_long_term_memories(memory_ids=memory_ids)


def create_sse_app():
    sse = SseServerTransport("/messages")

    async def handle_sse(request):
        async with sse.connect_sse(
            request.scope,
            request.receive,
            request._send,
        ) as (read_stream, write_stream):
            await mcp._mcp_server.run(
                read_stream,
                write_stream,
                mcp._mcp_server.create_initialization_options(),
            )

    return Starlette(
        debug=True,
        routes=[
            Route("/sse", endpoint=handle_sse),
            Mount("/messages", app=sse.handle_post_message),
        ],
    )


if __name__ == "__main__":
    if MCP_TRANSPORT == "sse":
        logger.info(f"Starting MCP server on port {MCP_PORT} (SSE mode)")
        app = create_sse_app()
        uvicorn.run(app, host="0.0.0.0", port=MCP_PORT)
    else:
        # Default to stdio
        logger.info("Starting MCP server (stdio mode)")
        mcp.run()
