"""Application constants"""

import os

# Slack message limits
SLACK_BLOCK_LIMIT = 2900  # Characters per block
SLACK_TEXT_SUMMARY_LENGTH = 200  # Characters for message text field
PREVIEW_MAX_LENGTH = 1000  # Characters for streaming preview

# User input limits
MAX_MESSAGE_LENGTH = 4000
MIN_MESSAGE_LENGTH = 1

# UI update timing
UPDATE_THROTTLE_SECONDS = 2  # Seconds between streaming UI updates

# Agent discovery
AGENT_CACHE_TTL = 300  # 5 minutes

# Session ID format
SESSION_ID_PREFIX = "slack"

# Default agent (fallback) - can be overridden via env vars
DEFAULT_AGENT_NAMESPACE = os.getenv("DEFAULT_AGENT_NAMESPACE", "kagent")
DEFAULT_AGENT_NAME = os.getenv("DEFAULT_AGENT_NAME", "k8s-agent")

# Emojis for UX
EMOJI_ROBOT = ":robot_face:"
EMOJI_THINKING = ":thinking_face:"
EMOJI_CLOCK = ":clock1:"
