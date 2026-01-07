"""Unit tests for context variable module."""

import pytest

from kagent.adk._context import clear_user_id, get_user_id, set_user_id


def test_get_user_id_default():
    """Test that get_user_id returns None by default."""
    # Ensure context is cleared
    clear_user_id()
    assert get_user_id() is None


def test_set_and_get_user_id():
    """Test setting and getting user_id."""
    test_user_id = "user@example.com"
    set_user_id(test_user_id)
    try:
        assert get_user_id() == test_user_id
    finally:
        clear_user_id()


def test_clear_user_id():
    """Test clearing user_id from context."""
    test_user_id = "user@example.com"
    set_user_id(test_user_id)
    assert get_user_id() == test_user_id

    clear_user_id()
    assert get_user_id() is None


def test_set_user_id_overwrites_previous():
    """Test that setting user_id overwrites the previous value."""
    set_user_id("user1@example.com")
    try:
        assert get_user_id() == "user1@example.com"

        set_user_id("user2@example.com")
        assert get_user_id() == "user2@example.com"
    finally:
        clear_user_id()


@pytest.mark.asyncio
async def test_context_isolation():
    """Test that context variables are isolated per async task."""
    import asyncio

    async def task1():
        set_user_id("user1@example.com")
        await asyncio.sleep(0.01)
        return get_user_id()

    async def task2():
        set_user_id("user2@example.com")
        await asyncio.sleep(0.01)
        return get_user_id()

    # Run tasks concurrently
    results = await asyncio.gather(task1(), task2())

    # Each task should see its own user_id
    assert results[0] == "user1@example.com"
    assert results[1] == "user2@example.com"

    # Main context should not have user_id set
    clear_user_id()
    assert get_user_id() is None
