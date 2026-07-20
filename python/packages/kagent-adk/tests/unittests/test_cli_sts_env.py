"""Tests for STS resource/audience env parsing in the CLI."""

import pytest

from kagent.adk.cli import _split_csv


@pytest.mark.parametrize(
    "value,expected",
    [
        (None, None),
        ("", None),
        ("   ", None),
        (",,", None),
        ("https://mcp.example.com", ["https://mcp.example.com"]),
        (" https://mcp.example.com ", ["https://mcp.example.com"]),
        ("https://a.example.com,https://b.example.com", ["https://a.example.com", "https://b.example.com"]),
        ("a, ,b,", ["a", "b"]),
    ],
)
def test_split_csv(value, expected):
    assert _split_csv(value) == expected
