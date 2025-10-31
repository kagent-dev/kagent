"""Tests for file operation tools."""

import tempfile
from pathlib import Path

from kagent.openai.agent.tools import EDIT_FILE_TOOL, READ_FILE_TOOL, WRITE_FILE_TOOL


class TestReadFileTool:
    """Tests for read_file tool."""

    def test_read_file_success(self):
        """Test reading a file successfully."""
        with tempfile.NamedTemporaryFile(mode="w", delete=False, suffix=".txt") as f:
            f.write("Line 1\nLine 2\nLine 3\n")
            temp_path = f.name

        try:
            # Invoke the tool
            import asyncio

            result = asyncio.run(
                READ_FILE_TOOL.on_invoke_tool(None, f'{{"file_path": "{temp_path}"}}')  # type: ignore
            )

            assert "     1|Line 1" in result
            assert "     2|Line 2" in result
            assert "     3|Line 3" in result
        finally:
            Path(temp_path).unlink()

    def test_read_file_not_found(self):
        """Test reading a non-existent file."""
        import asyncio

        result = asyncio.run(
            READ_FILE_TOOL.on_invoke_tool(
                None,
                '{"file_path": "/nonexistent/file.txt"}',  # type: ignore
            )
        )
        # The error is handled by the tool error function
        assert "File not found" in result or "error" in result.lower()


class TestWriteFileTool:
    """Tests for write_file tool."""

    def test_write_file_success(self):
        """Test writing a file successfully."""
        with tempfile.TemporaryDirectory() as tmpdir:
            temp_path = Path(tmpdir) / "test.txt"

            import asyncio

            result = asyncio.run(
                WRITE_FILE_TOOL.on_invoke_tool(
                    None,  # type: ignore
                    f'{{"file_path": "{temp_path}", "content": "Hello, World!"}}',
                )
            )

            assert "Successfully wrote to" in result
            assert temp_path.read_text() == "Hello, World!"


class TestEditFileTool:
    """Tests for edit_file tool."""

    def test_edit_file_success(self):
        """Test editing a file successfully."""
        with tempfile.NamedTemporaryFile(mode="w", delete=False, suffix=".txt") as f:
            f.write("Hello, World!")
            temp_path = f.name

        try:
            import asyncio

            result = asyncio.run(
                EDIT_FILE_TOOL.on_invoke_tool(
                    None,  # type: ignore
                    f'{{"file_path": "{temp_path}", "old_string": "World", "new_string": "Python"}}',
                )
            )

            assert "Successfully replaced 1 occurrence" in result
            assert Path(temp_path).read_text() == "Hello, Python!"
        finally:
            Path(temp_path).unlink()

    def test_edit_file_not_unique(self):
        """Test editing fails when old_string is not unique."""
        with tempfile.NamedTemporaryFile(mode="w", delete=False, suffix=".txt") as f:
            f.write("test test test")
            temp_path = f.name

        try:
            import asyncio

            result = asyncio.run(
                EDIT_FILE_TOOL.on_invoke_tool(
                    None,  # type: ignore
                    f'{{"file_path": "{temp_path}", "old_string": "test", "new_string": "replaced"}}',
                )
            )
            # The error is handled by the tool error function
            assert "appears 3 times" in result or "error" in result.lower()
        finally:
            Path(temp_path).unlink()

    def test_edit_file_replace_all(self):
        """Test editing with replace_all=True."""
        with tempfile.NamedTemporaryFile(mode="w", delete=False, suffix=".txt") as f:
            f.write("test test test")
            temp_path = f.name

        try:
            import asyncio

            result = asyncio.run(
                EDIT_FILE_TOOL.on_invoke_tool(
                    None,  # type: ignore
                    f'{{"file_path": "{temp_path}", '
                    f'"old_string": "test", "new_string": "replaced", "replace_all": true}}',
                )
            )

            assert "Successfully replaced 3 occurrence" in result
            assert Path(temp_path).read_text() == "replaced replaced replaced"
        finally:
            Path(temp_path).unlink()
