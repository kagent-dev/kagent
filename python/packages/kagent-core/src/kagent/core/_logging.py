import logging
import os

_logging_configured = False


def configure_logging() -> None:
    """Configure logging based on LOG_LEVEL environment variable."""
    global _logging_configured

    log_level = os.getenv("LOG_LEVEL", "INFO").upper()

    # Only configure if not already configured (avoid duplicate handlers)
    if not logging.root.handlers:
        logging.basicConfig(
            level=log_level,
            format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
        )
        _logging_configured = True
        logging.info(f"Logging configured with level: {log_level}")
    elif not _logging_configured:
        # Update level if already configured but we haven't logged yet
        logging.root.setLevel(log_level)
        _logging_configured = True
        logging.info(f"Logging level updated to: {log_level}")
    else:
        # Already configured and logged, just update the level silently
        logging.root.setLevel(log_level)
