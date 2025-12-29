"""Utilities for configuring A2A payload size limits."""

import logging

logger = logging.getLogger(__name__)

# Track the last patched value to detect conflicts
_last_patched_value: int | None = None


def patch_a2a_payload_limit(max_body_size: int) -> None:
    """Attempt to patch a2a-python library's hardcoded payload size limit.

    This function attempts to patch the a2a-python library's internal payload
    size limit by modifying the MAX_PAYLOAD_SIZE or _MAX_PAYLOAD_SIZE constant
    in the jsonrpc_app module.

    Args:
        max_body_size: Maximum payload size in bytes to set. Must be positive (> 0).

    Raises:
        ValueError: If max_body_size is not positive.

    Note:
        **IMPORTANT LIMITATION**: This function modifies a global module-level constant
        in the a2a-python library. In scenarios where multiple KAgentApp instances are
        created in the same process with different max_payload_size values, only the
        last value set will be effective. This is a process-level setting, not per-agent.

        This function gracefully handles cases where:
        - The jsonrpc_app module cannot be imported
        - The payload size constant doesn't exist
        - The module structure has changed

        In such cases, it logs a debug message and continues without raising
        an exception.

        If called multiple times with different values, a warning will be logged
        indicating that the previous value is being overridden.
    """
    global _last_patched_value

    if max_body_size <= 0:
        raise ValueError(f"max_body_size must be positive, got {max_body_size}")

    # Warn if patching with a different value than previously set
    if _last_patched_value is not None and _last_patched_value != max_body_size:
        logger.warning(
            f"Overriding previously patched max_payload_size ({_last_patched_value} bytes) "
            f"with new value ({max_body_size} bytes). This is a process-level setting - "
            f"all agents in this process will use the new value."
        )

    try:
        # Try different import paths for jsonrpc_app module
        jsonrpc_app = None
        import_paths = [
            "a2a.server.apps.jsonrpc.jsonrpc_app",
            "a2a.server.apps.jsonrpc_app",
        ]
        for path in import_paths:
            try:
                jsonrpc_app = __import__(path, fromlist=[""])
                break
            except ImportError:
                continue

        if jsonrpc_app is None:
            logger.debug("Could not find a2a-python jsonrpc_app module to patch")
            return

        # Check if MAX_PAYLOAD_SIZE or similar constant exists
        if hasattr(jsonrpc_app, "MAX_PAYLOAD_SIZE"):
            jsonrpc_app.MAX_PAYLOAD_SIZE = max_body_size
            logger.info(f"Patched a2a-python MAX_PAYLOAD_SIZE to {max_body_size} bytes")
            _last_patched_value = max_body_size
        # Also check for _MAX_PAYLOAD_SIZE or other variants
        elif hasattr(jsonrpc_app, "_MAX_PAYLOAD_SIZE"):
            jsonrpc_app._MAX_PAYLOAD_SIZE = max_body_size
            logger.info(f"Patched a2a-python _MAX_PAYLOAD_SIZE to {max_body_size} bytes")
            _last_patched_value = max_body_size
        else:
            logger.debug("Could not find MAX_PAYLOAD_SIZE constant in a2a-python jsonrpc_app")
    except (ImportError, AttributeError) as e:
        # If patching fails, log a debug message but continue
        logger.debug(f"Could not patch a2a-python payload limit: {e}")
