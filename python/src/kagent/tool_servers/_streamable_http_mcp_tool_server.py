from autogen_core import Component
from autogen_ext.tools.mcp._config import StreamableHttpServerParams
from autogen_ext.tools.mcp._factory import mcp_server_tools
from loguru import logger

from ._tool_server import ToolServer


class StreamableHttpMcpToolServerConfig(StreamableHttpServerParams):
    pass


class StreamableHttpMcpToolServer(ToolServer, Component[StreamableHttpMcpToolServerConfig]):
    component_config_schema = StreamableHttpMcpToolServerConfig
    component_type = "tool_server"
    component_provider_override = "kagent.tool_servers.StreamableHttpMcpToolServer"

    def __init__(self, config: StreamableHttpMcpToolServerConfig):
        self.config = config

    async def discover_tools(self) -> list[Component]:
        try:
            logger.debug(f"Discovering tools from streamable http server: {self.config}")
            tools = await mcp_server_tools(self.config)
            return tools
        except Exception as e:
            raise Exception(f"Failed to discover tools: {e}") from e

    def _to_config(self) -> StreamableHttpMcpToolServerConfig:
        return StreamableHttpMcpToolServerConfig(**self.config.model_dump())

    @classmethod
    def _from_config(cls, config: StreamableHttpMcpToolServerConfig):
        return cls(config)
