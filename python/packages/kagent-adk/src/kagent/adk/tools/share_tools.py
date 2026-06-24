"""Share link tools for agents to manage session sharing."""

from __future__ import annotations

import logging
import os
from typing import Any, Dict

import httpx
from google.adk.tools import BaseTool, ToolContext
from google.genai import types
from kagent.core.a2a import get_request_user_id

logger = logging.getLogger("kagent_adk." + __name__)

_KAGENT_UI_URL = os.getenv("KAGENT_UI_URL", "").rstrip("/")


def _parse_app_name(app_name: str) -> tuple[str, str]:
    """Parse a Python-identifier app_name back to (namespace, name)."""
    parts = app_name.split("__NS__", 1)
    if len(parts) != 2:
        return ("", app_name.replace("_", "-"))
    return (parts[0].replace("_", "-"), parts[1].replace("_", "-"))


def _share_url(token: str, session_id: str, app_name: str) -> str:
    """Return the share URL for the current session.

    When KAGENT_UI_URL is set, returns the full URL. Otherwise returns the
    relative path ``/agents/<namespace>/<name>/chat/<session_id>?share=<token>``.
    """
    namespace, name = _parse_app_name(app_name)
    path = f"/agents/{namespace}/{name}/chat/{session_id}?share={token}"
    return f"{_KAGENT_UI_URL}{path}" if _KAGENT_UI_URL else path


def _request_headers(app_name: str) -> Dict[str, str]:
    headers: Dict[str, str] = {"X-Agent-Name": app_name}
    user_id = get_request_user_id()
    if user_id:
        headers["X-User-Id"] = user_id
    return headers


class CreateShareLinkTool(BaseTool):
    """Create a share link for the current session.

    The link allows any authenticated user to view (and optionally interact with) the session.
    """

    def __init__(self, client: httpx.AsyncClient) -> None:
        super().__init__(
            name="create_share_link",
            description=(
                "Creates a share link for the current chat session. "
                "Returns a URL any authenticated user can open to view this session. "
                "The link is read-only by default (visitors cannot send messages). "
                "Set read_only=false to allow visitors to interact. "
                "Each call creates a new token; existing tokens remain valid."
            ),
        )
        self._client = client

    def _get_declaration(self) -> types.FunctionDeclaration:
        return types.FunctionDeclaration(
            name=self.name,
            description=self.description,
            parameters=types.Schema(
                type=types.Type.OBJECT,
                properties={
                    "read_only": types.Schema(
                        type=types.Type.BOOLEAN,
                        description="When true, the shared link will be read-only (visitors cannot send messages). Defaults to true.",
                    ),
                },
            ),
        )

    async def run_async(self, *, args: Dict[str, Any], tool_context: ToolContext) -> str:
        session_id = tool_context.session.id
        if not session_id or not session_id.strip():
            return "Error: session ID is empty — cannot create share link."
        app_name = tool_context.session.app_name
        read_only = bool(args.get("read_only", True))
        try:
            response = await self._client.post(
                f"/api/sessions/{session_id}/shares",
                headers=_request_headers(app_name),
                json={"read_only": read_only},
            )
            if response.status_code == 201:
                data = response.json().get("data", {})
                token = data.get("token", "")
                suffix = " (read-only)" if read_only else ""
                return f"Share link created{suffix}: {_share_url(token, session_id, app_name)}"
            return f"Failed to create share link: HTTP {response.status_code}: {response.text}"
        except httpx.TimeoutException as e:
            logger.error("Timeout creating share link: %s", e)
            return "Error creating share link: request timed out"
        except httpx.RequestError as e:
            logger.error("Request error creating share link: %s", e)
            return f"Error creating share link: {e}"
        except Exception as e:
            logger.error("Error creating share link: %s", e)
            return f"Error creating share link: {e}"


class ListShareLinksTool(BaseTool):
    """List existing share links for the current session."""

    def __init__(self, client: httpx.AsyncClient) -> None:
        super().__init__(
            name="list_share_links",
            description=(
                "Lists all active share links for the current session. "
                "Returns each share token and creation time. "
                "Use this to find a token before calling delete_share_link."
            ),
        )
        self._client = client

    def _get_declaration(self) -> types.FunctionDeclaration:
        return types.FunctionDeclaration(
            name=self.name,
            description=self.description,
            parameters=types.Schema(type=types.Type.OBJECT, properties={}),
        )

    async def run_async(self, *, args: Dict[str, Any], tool_context: ToolContext) -> str:
        session_id = tool_context.session.id
        if not session_id or not session_id.strip():
            return "Error: session ID is empty — cannot list share links."
        app_name = tool_context.session.app_name
        try:
            response = await self._client.get(
                f"/api/sessions/{session_id}/shares",
                headers=_request_headers(app_name),
            )
            if response.status_code == 200:
                shares = response.json().get("data", [])
                if not shares:
                    return "No active share links for this session."
                lines = [
                    f"- token: {s.get('token', '<unknown>')}, created_at: {s.get('created_at', 'unknown')}"
                    for s in shares
                ]
                return "Active share links:\n" + "\n".join(lines)
            return f"Failed to list share links: HTTP {response.status_code}: {response.text}"
        except httpx.TimeoutException as e:
            logger.error("Timeout listing share links: %s", e)
            return "Error listing share links: request timed out"
        except httpx.RequestError as e:
            logger.error("Request error listing share links: %s", e)
            return f"Error listing share links: {e}"
        except Exception as e:
            logger.error("Error listing share links: %s", e)
            return f"Error listing share links: {e}"


class DeleteShareLinkTool(BaseTool):
    """Delete a share link for the current session, revoking visitor access."""

    def __init__(self, client: httpx.AsyncClient) -> None:
        super().__init__(
            name="delete_share_link",
            description=(
                "Deletes a share link by token, immediately revoking access for anyone using it. "
                "Use list_share_links first to find the token you want to revoke."
            ),
        )
        self._client = client

    def _get_declaration(self) -> types.FunctionDeclaration:
        return types.FunctionDeclaration(
            name=self.name,
            description=self.description,
            parameters=types.Schema(
                type=types.Type.OBJECT,
                properties={
                    "token": types.Schema(
                        type=types.Type.STRING,
                        description="The share token to revoke.",
                    ),
                },
                required=["token"],
            ),
        )

    async def run_async(self, *, args: Dict[str, Any], tool_context: ToolContext) -> str:
        session_id = tool_context.session.id
        if not session_id or not session_id.strip():
            return "Error: session ID is empty — cannot delete share link."
        app_name = tool_context.session.app_name
        token = args.get("token", "").strip()
        if not token:
            return "Error: token is required."
        try:
            response = await self._client.delete(
                f"/api/sessions/{session_id}/shares/{token}",
                headers=_request_headers(app_name),
            )
            if response.status_code == 200:
                return f"Share link {token!r} revoked successfully."
            return f"Failed to delete share link: HTTP {response.status_code}: {response.text}"
        except httpx.TimeoutException as e:
            logger.error("Timeout deleting share link: %s", e)
            return "Error deleting share link: request timed out"
        except httpx.RequestError as e:
            logger.error("Request error deleting share link: %s", e)
            return f"Error deleting share link: {e}"
        except Exception as e:
            logger.error("Error deleting share link: %s", e)
            return f"Error deleting share link: {e}"
