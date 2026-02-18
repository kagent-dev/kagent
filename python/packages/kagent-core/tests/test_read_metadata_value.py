import pytest

from kagent.core.a2a import read_metadata_value


class TestReadMetadataValue:
    """Tests for the dual-prefix metadata reader."""

    def test_reads_kagent_prefix(self):
        metadata = {"kagent_type": "function_call"}
        assert read_metadata_value(metadata, "type") == "function_call"

    def test_reads_adk_prefix(self):
        metadata = {"adk_type": "function_call"}
        assert read_metadata_value(metadata, "type") == "function_call"

    def test_adk_takes_priority_when_both_present(self):
        metadata = {"adk_type": "adk_value", "kagent_type": "kagent_value"}
        assert read_metadata_value(metadata, "type") == "adk_value"

    def test_returns_default_for_missing_key(self):
        metadata = {"unrelated_key": "val"}
        assert read_metadata_value(metadata, "type") is None
        assert read_metadata_value(metadata, "type", "fallback") == "fallback"

    def test_returns_default_for_none_metadata(self):
        assert read_metadata_value(None, "type") is None
        assert read_metadata_value(None, "type", "default") == "default"

    def test_returns_default_for_empty_metadata(self):
        assert read_metadata_value({}, "type") is None
        assert read_metadata_value({}, "type", 42) == 42

    def test_raises_for_empty_key(self):
        with pytest.raises(ValueError, match="empty"):
            read_metadata_value({"a": 1}, "")

    def test_raises_for_none_key(self):
        with pytest.raises(ValueError, match="empty"):
            read_metadata_value({"a": 1}, None)  # type: ignore[arg-type]

    def test_preserves_non_string_values(self):
        metadata = {"kagent_usage": {"total": 100}}
        result = read_metadata_value(metadata, "usage")
        assert result == {"total": 100}

    def test_returns_false_value_not_default(self):
        """Ensure falsy values (False, 0, '') are returned, not treated as missing."""
        metadata = {"kagent_flag": False}
        assert read_metadata_value(metadata, "flag") is False

        metadata2 = {"adk_count": 0}
        assert read_metadata_value(metadata2, "count") == 0
