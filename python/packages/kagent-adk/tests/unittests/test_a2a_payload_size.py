# Copyright 2025 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Unit tests for A2A payload size configuration.

NOTE: These tests verify that the configuration value can be set and patched,
but do NOT verify end-to-end behavior (i.e., that payloads of the configured
size can actually be sent/received).
"""

from unittest import mock

import pytest

from kagent.core.a2a import patch_a2a_payload_limit


class TestPatchA2APayloadLimit:
    """Tests for patch_a2a_payload_limit function."""

    def test_patch_max_payload_size_exists(self):
        """Test patching when MAX_PAYLOAD_SIZE constant exists."""
        mock_jsonrpc_app = mock.MagicMock()
        mock_jsonrpc_app.MAX_PAYLOAD_SIZE = 7 * 1024 * 1024  # 7MB default

        with mock.patch("builtins.__import__", return_value=mock_jsonrpc_app):
            patch_a2a_payload_limit(50 * 1024 * 1024)  # 50MB

        assert mock_jsonrpc_app.MAX_PAYLOAD_SIZE == 50 * 1024 * 1024

    def test_patch_underscore_max_payload_size_exists(self):
        """Test patching when _MAX_PAYLOAD_SIZE constant exists."""
        mock_jsonrpc_app = mock.MagicMock()
        mock_jsonrpc_app._MAX_PAYLOAD_SIZE = 7 * 1024 * 1024  # 7MB default
        del mock_jsonrpc_app.MAX_PAYLOAD_SIZE  # Ensure MAX_PAYLOAD_SIZE doesn't exist

        with mock.patch("builtins.__import__", return_value=mock_jsonrpc_app):
            patch_a2a_payload_limit(100 * 1024 * 1024)  # 100MB

        assert mock_jsonrpc_app._MAX_PAYLOAD_SIZE == 100 * 1024 * 1024

    def test_patch_module_not_found(self):
        """Test behavior when jsonrpc_app module cannot be imported."""

        def mock_import(name, fromlist=None):
            if "jsonrpc" in name:
                raise ImportError("No module named 'a2a.server.apps.jsonrpc'")
            return mock.MagicMock()

        with mock.patch("builtins.__import__", side_effect=mock_import):
            # Should not raise an exception, just log debug message
            patch_a2a_payload_limit(50 * 1024 * 1024)

    def test_patch_no_payload_size_constant(self):
        """Test behavior when payload size constant doesn't exist."""
        mock_jsonrpc_app = mock.MagicMock()
        # Remove any payload size attributes
        if hasattr(mock_jsonrpc_app, "MAX_PAYLOAD_SIZE"):
            delattr(mock_jsonrpc_app, "MAX_PAYLOAD_SIZE")
        if hasattr(mock_jsonrpc_app, "_MAX_PAYLOAD_SIZE"):
            delattr(mock_jsonrpc_app, "_MAX_PAYLOAD_SIZE")

        with mock.patch("builtins.__import__", return_value=mock_jsonrpc_app):
            # Should not raise an exception, just log debug message
            patch_a2a_payload_limit(50 * 1024 * 1024)

    def test_patch_with_different_import_paths(self):
        """Test that function tries multiple import paths."""
        import_calls = []

        def mock_import(name, fromlist=None):
            import_calls.append(name)
            if name == "a2a.server.apps.jsonrpc.jsonrpc_app":
                raise ImportError("First path failed")
            if name == "a2a.server.apps.jsonrpc_app":
                mock_jsonrpc_app = mock.MagicMock()
                mock_jsonrpc_app.MAX_PAYLOAD_SIZE = 7 * 1024 * 1024
                return mock_jsonrpc_app
            return mock.MagicMock()

        with mock.patch("builtins.__import__", side_effect=mock_import):
            patch_a2a_payload_limit(50 * 1024 * 1024)

        # Should have tried both import paths
        assert "a2a.server.apps.jsonrpc.jsonrpc_app" in import_calls
        assert "a2a.server.apps.jsonrpc_app" in import_calls

    def test_patch_raises_error_for_zero(self):
        """Test that patch_a2a_payload_limit raises ValueError for zero."""
        with pytest.raises(ValueError, match="must be positive"):
            patch_a2a_payload_limit(0)

    def test_patch_raises_error_for_negative(self):
        """Test that patch_a2a_payload_limit raises ValueError for negative values."""
        with pytest.raises(ValueError, match="must be positive"):
            patch_a2a_payload_limit(-1)
        with pytest.raises(ValueError, match="must be positive"):
            patch_a2a_payload_limit(-100 * 1024 * 1024)

    def test_patch_warns_on_override(self, caplog):
        """Test that patching with a different value logs a warning."""
        mock_jsonrpc_app = mock.MagicMock()
        mock_jsonrpc_app.MAX_PAYLOAD_SIZE = 7 * 1024 * 1024  # 7MB default

        with mock.patch("builtins.__import__", return_value=mock_jsonrpc_app):
            # First patch
            patch_a2a_payload_limit(50 * 1024 * 1024)  # 50MB
            assert mock_jsonrpc_app.MAX_PAYLOAD_SIZE == 50 * 1024 * 1024

            # Second patch with different value - should warn
            patch_a2a_payload_limit(100 * 1024 * 1024)  # 100MB
            assert mock_jsonrpc_app.MAX_PAYLOAD_SIZE == 100 * 1024 * 1024

        # Check that warning was logged
        assert any(
            "Overriding previously patched" in record.message and "process-level setting" in record.message
            for record in caplog.records
        )

    def test_patch_no_warning_on_same_value(self, caplog):
        """Test that patching with the same value doesn't log a warning."""
        mock_jsonrpc_app = mock.MagicMock()
        mock_jsonrpc_app.MAX_PAYLOAD_SIZE = 7 * 1024 * 1024  # 7MB default

        with mock.patch("builtins.__import__", return_value=mock_jsonrpc_app):
            # First patch
            patch_a2a_payload_limit(50 * 1024 * 1024)  # 50MB

            # Second patch with same value - should not warn
            patch_a2a_payload_limit(50 * 1024 * 1024)  # 50MB again

        # Check that no warning was logged
        assert not any("Overriding previously patched" in record.message for record in caplog.records)
