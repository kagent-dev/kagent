"""Tests for MCP tool confirmation logic in KAgentMcpToolset.

Tests the _should_require_confirmation() function and confirmation behavior
with various MCP annotation combinations and exception rules.
"""

from unittest.mock import MagicMock


from kagent.adk._mcp_toolset import _should_require_confirmation
from kagent.adk.types import ToolConfirmationConfig


def make_mock_tool(
    name: str,
    read_only: bool | None = None,
    destructive: bool | None = None,
    idempotent: bool | None = None,
    has_annotations: bool = True,
) -> MagicMock:
    """Create a mock tool with controllable MCP annotations.

    Args:
        name: Tool name
        read_only: Value for readOnlyHint annotation (None = not set)
        destructive: Value for destructiveHint annotation (None = not set)
        idempotent: Value for idempotentHint annotation (None = not set)
        has_annotations: Whether the tool has _mcp_tool.annotations

    Returns:
        A mock BaseTool instance with controllable annotations
    """
    tool = MagicMock()
    tool.name = name

    if has_annotations:
        annotations = MagicMock()
        annotations.readOnlyHint = read_only
        annotations.destructiveHint = destructive
        annotations.idempotentHint = idempotent
        tool._mcp_tool = MagicMock()
        tool._mcp_tool.annotations = annotations
    else:
        tool._mcp_tool = MagicMock()
        tool._mcp_tool.annotations = None

    return tool


class TestShouldRequireConfirmationBasics:
    """Tests for basic confirmation behavior without exceptions."""

    def test_confirm_none_no_confirmation(self):
        """When confirm_config is None, no tools require confirmation."""
        tool = make_mock_tool("my_tool")
        assert _should_require_confirmation(tool, None) is False

    def test_confirm_empty_all_require(self):
        """When confirm_config is empty (no exceptions), all tools require confirmation."""
        tool = make_mock_tool("my_tool")
        confirm_config = ToolConfirmationConfig()
        assert _should_require_confirmation(tool, confirm_config) is True

    def test_no_annotations_all_require(self):
        """When tool has no annotations and confirm is set, confirmation is required."""
        tool = make_mock_tool("my_tool", has_annotations=False)
        confirm_config = ToolConfirmationConfig()
        assert _should_require_confirmation(tool, confirm_config) is True

    def test_tool_without_mcp_tool_attribute(self):
        """When tool doesn't have _mcp_tool attribute, confirmation is required."""
        tool = MagicMock()
        tool.name = "my_tool"
        # Don't set _mcp_tool attribute at all
        delattr(tool, "_mcp_tool")

        confirm_config = ToolConfirmationConfig()
        assert _should_require_confirmation(tool, confirm_config) is True


class TestExceptReadOnly:
    """Tests for except_read_only exception rule."""

    def test_except_read_only_skips_readonly_tools(self):
        """When except_read_only=True and readOnlyHint=True, skip confirmation."""
        tool = make_mock_tool("readonly_tool", read_only=True)
        confirm_config = ToolConfirmationConfig(except_read_only=True)
        assert _should_require_confirmation(tool, confirm_config) is False

    def test_except_read_only_keeps_non_readonly(self):
        """When except_read_only=True and readOnlyHint=False, require confirmation."""
        tool = make_mock_tool("non_readonly_tool", read_only=False)
        confirm_config = ToolConfirmationConfig(except_read_only=True)
        assert _should_require_confirmation(tool, confirm_config) is True

    def test_except_read_only_none_annotation_keeps_confirmation(self):
        """When except_read_only=True and readOnlyHint=None, require confirmation.

        MCP default for readOnlyHint is False, so None means not read-only.
        """
        tool = make_mock_tool("tool", read_only=None)
        confirm_config = ToolConfirmationConfig(except_read_only=True)
        assert _should_require_confirmation(tool, confirm_config) is True

    def test_except_read_only_false_does_not_skip(self):
        """When except_read_only=False, readOnlyHint=True doesn't skip confirmation."""
        tool = make_mock_tool("readonly_tool", read_only=True)
        confirm_config = ToolConfirmationConfig(except_read_only=False)
        assert _should_require_confirmation(tool, confirm_config) is True

    def test_except_read_only_none_does_not_skip(self):
        """When except_read_only=None, readOnlyHint=True doesn't skip confirmation."""
        tool = make_mock_tool("readonly_tool", read_only=True)
        confirm_config = ToolConfirmationConfig(except_read_only=None)
        assert _should_require_confirmation(tool, confirm_config) is True


class TestExceptIdempotent:
    """Tests for except_idempotent exception rule."""

    def test_except_idempotent_skips_idempotent(self):
        """When except_idempotent=True and idempotentHint=True, skip confirmation."""
        tool = make_mock_tool("idempotent_tool", idempotent=True)
        confirm_config = ToolConfirmationConfig(except_idempotent=True)
        assert _should_require_confirmation(tool, confirm_config) is False

    def test_except_idempotent_keeps_non_idempotent(self):
        """When except_idempotent=True and idempotentHint=False, require confirmation."""
        tool = make_mock_tool("non_idempotent_tool", idempotent=False)
        confirm_config = ToolConfirmationConfig(except_idempotent=True)
        assert _should_require_confirmation(tool, confirm_config) is True

    def test_except_idempotent_none_annotation_keeps_confirmation(self):
        """When except_idempotent=True and idempotentHint=None, require confirmation.

        MCP default for idempotentHint is False, so None means not idempotent.
        """
        tool = make_mock_tool("tool", idempotent=None)
        confirm_config = ToolConfirmationConfig(except_idempotent=True)
        assert _should_require_confirmation(tool, confirm_config) is True

    def test_except_idempotent_false_does_not_skip(self):
        """When except_idempotent=False, idempotentHint=True doesn't skip confirmation."""
        tool = make_mock_tool("idempotent_tool", idempotent=True)
        confirm_config = ToolConfirmationConfig(except_idempotent=False)
        assert _should_require_confirmation(tool, confirm_config) is True

    def test_except_idempotent_none_does_not_skip(self):
        """When except_idempotent=None, idempotentHint=True doesn't skip confirmation."""
        tool = make_mock_tool("idempotent_tool", idempotent=True)
        confirm_config = ToolConfirmationConfig(except_idempotent=None)
        assert _should_require_confirmation(tool, confirm_config) is True


class TestExceptNonDestructive:
    """Tests for except_non_destructive exception rule."""

    def test_except_non_destructive_skips_non_destructive(self):
        """When except_non_destructive=True and destructiveHint=False, skip confirmation."""
        tool = make_mock_tool("safe_tool", destructive=False)
        confirm_config = ToolConfirmationConfig(except_non_destructive=True)
        assert _should_require_confirmation(tool, confirm_config) is False

    def test_except_non_destructive_keeps_destructive(self):
        """When except_non_destructive=True and destructiveHint=True, require confirmation."""
        tool = make_mock_tool("destructive_tool", destructive=True)
        confirm_config = ToolConfirmationConfig(except_non_destructive=True)
        assert _should_require_confirmation(tool, confirm_config) is True

    def test_except_non_destructive_none_annotation_keeps_confirmation(self):
        """When except_non_destructive=True and destructiveHint=None, require confirmation.

        MCP default for destructiveHint is True, so None means destructive.
        Only explicit False skips confirmation.
        """
        tool = make_mock_tool("tool", destructive=None)
        confirm_config = ToolConfirmationConfig(except_non_destructive=True)
        assert _should_require_confirmation(tool, confirm_config) is True

    def test_except_non_destructive_keeps_explicit_destructive(self):
        """When except_non_destructive=True and destructiveHint=True, require confirmation."""
        tool = make_mock_tool("destructive_tool", destructive=True)
        confirm_config = ToolConfirmationConfig(except_non_destructive=True)
        assert _should_require_confirmation(tool, confirm_config) is True

    def test_except_non_destructive_false_does_not_skip(self):
        """When except_non_destructive=False, destructiveHint=False doesn't skip confirmation."""
        tool = make_mock_tool("safe_tool", destructive=False)
        confirm_config = ToolConfirmationConfig(except_non_destructive=False)
        assert _should_require_confirmation(tool, confirm_config) is True

    def test_except_non_destructive_none_does_not_skip(self):
        """When except_non_destructive=None, destructiveHint=False doesn't skip confirmation."""
        tool = make_mock_tool("safe_tool", destructive=False)
        confirm_config = ToolConfirmationConfig(except_non_destructive=None)
        assert _should_require_confirmation(tool, confirm_config) is True


class TestExceptTools:
    """Tests for except_tools exception rule (tool name matching)."""

    def test_except_tools_by_name(self):
        """When except_tools contains tool name, skip confirmation."""
        tool = make_mock_tool("foo")
        confirm_config = ToolConfirmationConfig(except_tools=["foo"])
        assert _should_require_confirmation(tool, confirm_config) is False

    def test_except_tools_no_match(self):
        """When except_tools doesn't contain tool name, require confirmation."""
        tool = make_mock_tool("bar")
        confirm_config = ToolConfirmationConfig(except_tools=["foo"])
        assert _should_require_confirmation(tool, confirm_config) is True

    def test_except_tools_multiple_names(self):
        """When except_tools contains multiple names, match any of them."""
        tool = make_mock_tool("bar")
        confirm_config = ToolConfirmationConfig(except_tools=["foo", "bar", "baz"])
        assert _should_require_confirmation(tool, confirm_config) is False

    def test_except_tools_empty_list(self):
        """When except_tools is empty list, require confirmation."""
        tool = make_mock_tool("foo")
        confirm_config = ToolConfirmationConfig(except_tools=[])
        assert _should_require_confirmation(tool, confirm_config) is True

    def test_except_tools_case_sensitive(self):
        """Tool name matching is case-sensitive."""
        tool = make_mock_tool("Foo")
        confirm_config = ToolConfirmationConfig(except_tools=["foo"])
        assert _should_require_confirmation(tool, confirm_config) is True


class TestMultipleExceptions:
    """Tests for combinations of multiple exception rules."""

    def test_multiple_exceptions_first_match_wins(self):
        """When tool matches first exception rule, confirmation is skipped.

        Tool matches readOnly exception, so confirmation is skipped even if
        other exceptions don't match.
        """
        tool = make_mock_tool(
            "tool",
            read_only=True,
            destructive=True,
            idempotent=False,
        )
        confirm_config = ToolConfirmationConfig(
            except_read_only=True,
            except_idempotent=True,
            except_non_destructive=True,
        )
        assert _should_require_confirmation(tool, confirm_config) is False

    def test_multiple_exceptions_second_match_wins(self):
        """When tool matches second exception rule, confirmation is skipped."""
        tool = make_mock_tool(
            "tool",
            read_only=False,
            destructive=True,
            idempotent=True,
        )
        confirm_config = ToolConfirmationConfig(
            except_read_only=True,
            except_idempotent=True,
            except_non_destructive=True,
        )
        assert _should_require_confirmation(tool, confirm_config) is False

    def test_multiple_exceptions_name_match_wins(self):
        """When tool matches name exception, confirmation is skipped."""
        tool = make_mock_tool(
            "special_tool",
            read_only=False,
            destructive=True,
            idempotent=False,
        )
        confirm_config = ToolConfirmationConfig(
            except_read_only=True,
            except_idempotent=True,
            except_non_destructive=True,
            except_tools=["special_tool"],
        )
        assert _should_require_confirmation(tool, confirm_config) is False

    def test_all_exceptions_exempt_matching_tool(self):
        """When tool matches all exception rules, confirmation is skipped."""
        tool = make_mock_tool(
            "special_tool",
            read_only=True,
            destructive=False,
            idempotent=True,
        )
        confirm_config = ToolConfirmationConfig(
            except_read_only=True,
            except_idempotent=True,
            except_non_destructive=True,
            except_tools=["special_tool"],
        )
        assert _should_require_confirmation(tool, confirm_config) is False

    def test_no_exceptions_match_requires_confirmation(self):
        """When tool doesn't match any exception rule, confirmation is required."""
        tool = make_mock_tool(
            "tool",
            read_only=False,
            destructive=True,
            idempotent=False,
        )
        confirm_config = ToolConfirmationConfig(
            except_read_only=True,
            except_idempotent=True,
            except_non_destructive=True,
            except_tools=["other_tool"],
        )
        assert _should_require_confirmation(tool, confirm_config) is True


class TestAnnotationDefaults:
    """Tests for MCP annotation default behavior.

    MCP spec defaults:
    - readOnlyHint: False (not read-only)
    - destructiveHint: True (is destructive)
    - idempotentHint: False (not idempotent)
    """

    def test_all_annotations_none_requires_confirmation(self):
        """When all annotations are None, tool is treated as destructive."""
        tool = make_mock_tool("tool", read_only=None, destructive=None, idempotent=None)
        confirm_config = ToolConfirmationConfig(
            except_read_only=True,
            except_idempotent=True,
            except_non_destructive=True,
        )
        # Tool doesn't match any exception (not read-only, not idempotent, IS destructive)
        assert _should_require_confirmation(tool, confirm_config) is True

    def test_typical_destructive_tool(self):
        """Typical destructive tool with explicit annotations."""
        tool = make_mock_tool(
            "delete_resource",
            read_only=False,
            destructive=True,
            idempotent=False,
        )
        confirm_config = ToolConfirmationConfig(except_read_only=True)
        # Tool is not read-only, so doesn't match exception
        assert _should_require_confirmation(tool, confirm_config) is True

    def test_typical_readonly_tool(self):
        """Typical read-only tool with explicit annotations."""
        tool = make_mock_tool(
            "list_resources",
            read_only=True,
            destructive=False,
            idempotent=True,
        )
        confirm_config = ToolConfirmationConfig(except_read_only=True)
        # Tool is read-only, so matches exception
        assert _should_require_confirmation(tool, confirm_config) is False

    def test_idempotent_safe_tool(self):
        """Idempotent tool that's safe to run multiple times."""
        tool = make_mock_tool(
            "ensure_config",
            read_only=False,
            destructive=False,
            idempotent=True,
        )
        confirm_config = ToolConfirmationConfig(
            except_idempotent=True,
            except_non_destructive=True,
        )
        # Tool matches idempotent exception
        assert _should_require_confirmation(tool, confirm_config) is False


class TestEdgeCases:
    """Tests for edge cases and boundary conditions."""

    def test_tool_with_none_mcp_tool(self):
        """When _mcp_tool is None, treat as no annotations."""
        tool = MagicMock()
        tool.name = "tool"
        tool._mcp_tool = None

        confirm_config = ToolConfirmationConfig()
        assert _should_require_confirmation(tool, confirm_config) is True

    def test_tool_with_none_annotations_attribute(self):
        """When annotations attribute is None, treat as no annotations."""
        tool = MagicMock()
        tool.name = "tool"
        tool._mcp_tool = MagicMock()
        tool._mcp_tool.annotations = None

        confirm_config = ToolConfirmationConfig()
        assert _should_require_confirmation(tool, confirm_config) is True

    def test_except_tools_with_special_characters(self):
        """Tool names with special characters are matched exactly."""
        tool = make_mock_tool("tool-with-dashes")
        confirm_config = ToolConfirmationConfig(except_tools=["tool-with-dashes"])
        assert _should_require_confirmation(tool, confirm_config) is False

    def test_except_tools_with_underscores(self):
        """Tool names with underscores are matched exactly."""
        tool = make_mock_tool("tool_with_underscores")
        confirm_config = ToolConfirmationConfig(except_tools=["tool_with_underscores"])
        assert _should_require_confirmation(tool, confirm_config) is False

    def test_empty_tool_name(self):
        """Tool with empty name is handled correctly."""
        tool = make_mock_tool("")
        confirm_config = ToolConfirmationConfig(except_tools=[""])
        assert _should_require_confirmation(tool, confirm_config) is False

    def test_whitespace_in_tool_name(self):
        """Tool names with whitespace are matched exactly."""
        tool = make_mock_tool("tool with spaces")
        confirm_config = ToolConfirmationConfig(except_tools=["tool with spaces"])
        assert _should_require_confirmation(tool, confirm_config) is False

    def test_whitespace_mismatch_in_tool_name(self):
        """Tool names with different whitespace don't match."""
        tool = make_mock_tool("tool with spaces")
        confirm_config = ToolConfirmationConfig(except_tools=["tool  with  spaces"])
        assert _should_require_confirmation(tool, confirm_config) is True


class TestAnnotationMissingAttributes:
    """Tests for handling missing annotation attributes."""

    def test_missing_readOnlyHint_attribute(self):
        """When readOnlyHint attribute is missing, getattr returns None."""
        tool = MagicMock()
        tool.name = "tool"
        annotations = MagicMock(spec=[])  # Empty spec means no attributes
        tool._mcp_tool = MagicMock()
        tool._mcp_tool.annotations = annotations

        confirm_config = ToolConfirmationConfig(except_read_only=True)
        # Missing readOnlyHint is treated as None, so doesn't match exception
        assert _should_require_confirmation(tool, confirm_config) is True

    def test_missing_destructiveHint_attribute(self):
        """When destructiveHint attribute is missing, getattr returns None."""
        tool = MagicMock()
        tool.name = "tool"
        annotations = MagicMock(spec=[])  # Empty spec means no attributes
        tool._mcp_tool = MagicMock()
        tool._mcp_tool.annotations = annotations

        confirm_config = ToolConfirmationConfig(except_non_destructive=True)
        # Missing destructiveHint is treated as None, so doesn't match exception
        assert _should_require_confirmation(tool, confirm_config) is True

    def test_missing_idempotentHint_attribute(self):
        """When idempotentHint attribute is missing, getattr returns None."""
        tool = MagicMock()
        tool.name = "tool"
        annotations = MagicMock(spec=[])  # Empty spec means no attributes
        tool._mcp_tool = MagicMock()
        tool._mcp_tool.annotations = annotations

        confirm_config = ToolConfirmationConfig(except_idempotent=True)
        # Missing idempotentHint is treated as None, so doesn't match exception
        assert _should_require_confirmation(tool, confirm_config) is True
