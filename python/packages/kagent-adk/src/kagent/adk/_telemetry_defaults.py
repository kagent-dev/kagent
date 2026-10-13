"""ADK telemetry settings kagent applies when the environment leaves them unset."""

import os

from kagent.core.telemetry import _defaults

os.environ.setdefault("ADK_TELEMETRY_SCHEMA_VERSION_OPT_IN", "2")
os.environ.setdefault("ADK_CAPTURE_MESSAGE_CONTENT_IN_SPANS", "false")
