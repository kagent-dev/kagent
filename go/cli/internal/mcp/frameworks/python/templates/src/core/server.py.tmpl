"""Dynamic MCP server implementation with automatic tool discovery.

This server automatically discovers and loads tools from the tools directory.
Each tool file should contain a function decorated with @mcp.tool().
"""

import importlib.util
import logging
import sys
from pathlib import Path
from typing import Any

from dotenv import load_dotenv
from fastmcp import FastMCP

from .utils import load_config

# Global FastMCP instance for tools to import
mcp: FastMCP = FastMCP(name="Dynamic Server")


class DynamicMCPServer:
    """MCP server with dynamic tool loading capabilities."""

    def __init__(self, name: str, tools_dir: str = "src/tools"):
        """Initialize the dynamic MCP server.

        Args:
            name: Server name
            tools_dir: Directory containing tool files
        """
        global mcp
        self.name = name
        self.tools_dir = Path(tools_dir)
        self.config = self._load_config()

        # Load local environment variables if configured
        self._load_local_env()

        # Update global FastMCP instance
        mcp = FastMCP(name=self.name)
        self.mcp = mcp

        # Track loaded tools
        self.loaded_tools: list[str] = []

    def _load_config(self) -> dict[str, Any]:
        """Load configuration from manifest.yaml."""
        return load_config("manifest.yaml")

    def _load_local_env(self) -> None:
        """Load environment variables from a .env file if it exists."""
        # load_dotenv will search for a .env file and load it.
        # It does not fail if the file is not found.
        if load_dotenv(override=True):
            logging.info("Loaded environment variables from .env file")

    def load_tools(self) -> None:
        """Discover and load all tools from the tools directory."""
        if not self.tools_dir.exists():
            print(f"Tools directory {self.tools_dir} does not exist")
            return

        # Find all Python files in tools directory
        tool_files = list(self.tools_dir.glob("*.py"))
        tool_files = [f for f in tool_files if f.name != "__init__.py"]

        if not tool_files:
            logging.warning(f"No tool files found in {self.tools_dir}")
            return

        loaded_count = 0
        has_errors = False

        for tool_file in tool_files:
            try:
                # Get the number of tools before importing
                tools_before = len(self.mcp._tool_manager._tools)

                # Simply import the module - tools auto-register via @mcp.tool()
                # decorator
                tool_name = tool_file.stem
                if self._import_tool_module(tool_file, tool_name):
                    # Check if any tools were actually registered
                    tools_after = len(self.mcp._tool_manager._tools)
                    if tools_after > tools_before:
                        self.loaded_tools.append(tool_name)
                        loaded_count += 1
                        logging.info(f"Loaded tool module: {tool_name}")
                    else:
                        logging.error(
                            f"Tool file {tool_name} did not register any tools"
                        )
                        has_errors = True
                else:
                    logging.error(f"Failed to load tool module: {tool_name}")
                    has_errors = True

            except Exception as e:
                logging.error(f"Error loading tool {tool_file.name}: {e}")
                has_errors = True

        # Fail fast - if any tool fails to load, stop the server
        if has_errors:
            sys.exit(1)

        logging.info(f"📦 Successfully loaded {loaded_count} tools")

        if loaded_count == 0:
            logging.warning("No tools loaded. Server starting without tools.")

    def _import_tool_module(self, tool_file: Path, tool_name: str) -> bool:
        """Import a tool module, which auto-registers tools via decorators.

        Args:
            tool_file: Path to the tool file
            tool_name: Name of the tool (same as filename)

        Returns:
            True if module was imported successfully
        """
        try:
            # Load the module
            spec = importlib.util.spec_from_file_location(tool_name, tool_file)
            if spec is None or spec.loader is None:
                return False

            module = importlib.util.module_from_spec(spec)

            # Add to sys.modules so it can be imported by other modules
            sys.modules[f"tools.{tool_name}"] = module

            # Execute the module - this will trigger @mcp.tool() decorators
            spec.loader.exec_module(module)

            return True

        except Exception as e:
            print(f"Error importing {tool_file}: {e}")
            return False

    def get_tools_sync(self) -> dict[str, Any]:
        """Get tools synchronously for testing purposes."""
        # This is a simplified version for testing - in real usage, use get_tools()
        # async
        return self.mcp._tool_manager._tools

    def run(self, transport_mode: str = "stdio", host: str = "localhost", port: int = 3000) -> None:
        """Run the FastMCP server.

        Args:
            transport_mode: Transport mode - "stdio", or "http"
            host: Host to bind to in HTTP mode
            port: Port to bind to in HTTP mode
        """

        if transport_mode == "http":
            self.mcp.run(transport="http", host=host, port=port, path="/mcp")
        elif transport_mode == "stdio":
            # Default to stdio mode
            self.mcp.run()
