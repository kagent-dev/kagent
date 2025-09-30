import logging
from typing import Any, Optional

from google.adk.auth.auth_credential import AuthCredential, AuthCredentialTypes, HttpAuth, HttpCredentials
from google.adk.plugins.base_plugin import BasePlugin
from google.adk.tools.base_tool import BaseTool
from google.adk.tools.mcp_tool import MCPTool
from google.adk.tools.tool_context import ToolContext
from typing_extensions import override

from .wrapped_session_service import ACCESS_TOKEN_KEY

logger = logging.getLogger(__name__)


class TokenPropagationPlugin(BasePlugin):
    def __init__(self):
        super().__init__("TokenPropagationPlugin")

    @override
    async def before_tool_callback(
        self,
        *,
        tool: BaseTool,
        tool_args: dict[str, Any],
        tool_context: ToolContext,
    ) -> Optional[dict]:
        if isinstance(tool, MCPTool):
            logger.debug("Setting up token propagation for tool: %s", tool.name)
            token = tool_context._invocation_context.session.state.get(ACCESS_TOKEN_KEY, None)
            if token:
                credential = AuthCredential(
                    auth_type=AuthCredentialTypes.HTTP,
                    http=HttpAuth(
                        scheme="bearer",
                        credentials=HttpCredentials(token=token),
                    ),
                )
                logger.debug("Propagating token in tool call: %s", tool.name)
                return await tool._run_async_impl(args=tool_args, tool_context=tool_context, credential=credential)
        return None
