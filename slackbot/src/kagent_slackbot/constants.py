"""Application constants"""

import os

# Slack message limits
SLACK_BLOCK_LIMIT = 2900  # Characters per block

# User input limits
MAX_MESSAGE_LENGTH = 4000
MIN_MESSAGE_LENGTH = 1

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
