"""Tests for KAgentTokenService's background refresh loop."""

import asyncio
from unittest.mock import patch

import pytest

from kagent.adk._token import KAgentTokenService


@pytest.mark.asyncio
async def test_refresh_token_survives_unexpected_error():
    """One failing read must not permanently stop the refresh loop.

    Regression test: _refresh_token only guarded against a token read
    returning None; any other exception from the read used to propagate out
    of the while loop and kill the background task for the rest of the
    process's life.
    """
    service = KAgentTokenService(app_name="test-agent")
    service.token = "old-token"

    read_calls = 0

    async def fake_sleep(_seconds):
        return None

    async def fake_read():
        nonlocal read_calls
        read_calls += 1
        if read_calls == 1:
            raise ValueError("unexpected decode error")
        if read_calls == 2:
            return "new-token"
        # Stop the loop once the point is proven.
        raise asyncio.CancelledError

    with patch("asyncio.sleep", fake_sleep), patch.object(service, "_read_kagent_token", fake_read):
        with pytest.raises(asyncio.CancelledError):
            await service._refresh_token()

    assert read_calls == 3, "the loop must keep running after the first read raises"
    assert service.token == "new-token", "a later successful read must still update the token"
