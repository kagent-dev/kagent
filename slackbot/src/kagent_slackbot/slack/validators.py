"""Input validation and sanitization"""

import re

from ..constants import MAX_MESSAGE_LENGTH, MIN_MESSAGE_LENGTH


def validate_message(text: str) -> bool:
    """
    Validate user message

    Args:
        text: Message text

    Returns:
        True if valid, False otherwise
    """
    if not text or len(text.strip()) < MIN_MESSAGE_LENGTH:
        return False

    if len(text) > MAX_MESSAGE_LENGTH:
        return False

    return True


def sanitize_message(text: str) -> str:
    """
    Sanitize user message

    Args:
        text: Raw message text

    Returns:
        Sanitized text
    """
    text = text.strip()
    text = re.sub(r"\s+", " ", text)

    if len(text) > MAX_MESSAGE_LENGTH:
        text = text[:MAX_MESSAGE_LENGTH]

    return text


def strip_bot_mention(text: str) -> str:
    """
    Remove bot mention from text

    Args:
        text: Text with potential @bot mention

    Returns:
        Text without mention
    """
    text = re.sub(r"<@[A-Z0-9]+>", "", text)
    return text.strip()
