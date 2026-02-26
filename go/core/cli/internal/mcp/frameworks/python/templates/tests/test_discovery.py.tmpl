"""Tests for tool discovery and loading mechanism."""

import sys
import tempfile
from pathlib import Path

import pytest

# Add src to Python path
sys.path.insert(0, str(Path(__file__).parent.parent / "src"))

from core.server import DynamicMCPServer  # noqa: E402


class TestToolDiscovery:
    """Test the tool discovery mechanism."""

    def test_discover_tools_in_directory(self) -> None:
        """Test discovering tools in a directory."""
        with tempfile.TemporaryDirectory() as temp_dir:
            tools_dir = Path(temp_dir) / "tools"
            tools_dir.mkdir()

            # Create a test tool file
            tool_file = tools_dir / "test_tool.py"
            tool_content = '''
from core.server import mcp

@mcp.tool()
def test_tool(message: str) -> str:
    return f"Test: {message}"
'''
            tool_file.write_text(tool_content)

            # Test discovery
            server = DynamicMCPServer(name="Test", tools_dir=str(tools_dir))

            # Load tools - this should work without raising SystemExit
            try:
                server.load_tools()
                # If we get here, it means loading succeeded
                assert True
            except SystemExit:
                pytest.fail("Tool loading failed")

    def test_invalid_tool_fails_fast(self) -> None:
        """Test that invalid tools cause the server to exit."""
        with tempfile.TemporaryDirectory() as temp_dir:
            tools_dir = Path(temp_dir) / "tools"
            tools_dir.mkdir()

            # Create an invalid tool file (syntax error)
            tool_file = tools_dir / "invalid_tool.py"
            # This has a syntax error
            tool_content = 'syntax error'
            tool_file.write_text(tool_content)

            server = DynamicMCPServer(name="Test", tools_dir=str(tools_dir))

            # This should cause SystemExit due to fail-fast behavior
            with pytest.raises(SystemExit):
                server.load_tools()

    def test_tool_without_matching_function(self) -> None:
        """Test tool file without matching function name."""
        with tempfile.TemporaryDirectory() as temp_dir:
            tools_dir = Path(temp_dir) / "tools"
            tools_dir.mkdir()

            # Create a tool file without matching function name
            tool_file = tools_dir / "mismatch.py"
            tool_content = '''
def wrong_name(message: str) -> str:
    return f"Wrong: {message}"
'''
            tool_file.write_text(tool_content)

            server = DynamicMCPServer(name="Test", tools_dir=str(tools_dir))

            # This should cause SystemExit due to fail-fast behavior
            with pytest.raises(SystemExit):
                server.load_tools()

    def test_empty_tools_directory(self) -> None:
        """Test behavior with empty tools directory."""
        with tempfile.TemporaryDirectory() as temp_dir:
            tools_dir = Path(temp_dir) / "tools"
            tools_dir.mkdir()

            server = DynamicMCPServer(name="Test", tools_dir=str(tools_dir))

            # Should not raise exception
            server.load_tools()
            assert len(server.loaded_tools) == 0
