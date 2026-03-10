import logging
import os

_logging_configured = False

LOG_FORMAT = "%(asctime)s - %(name)s - %(levelname)s - %(message)s"


def configure_logging() -> None:
    """Configure logging based on LOG_LEVEL environment variable."""
    global _logging_configured

    log_level = os.getenv("LOG_LEVEL", "INFO").upper()
    formatter = logging.Formatter(LOG_FORMAT)

    # Only configure if not already configured (avoid duplicate handlers)
    if not logging.root.handlers:
        logging.basicConfig(
            level=log_level,
            format=LOG_FORMAT,
        )
        _logging_configured = True
        logging.info(f"Logging configured with level: {log_level}")
    elif not _logging_configured:
        # Update level and ensure timestamp format on existing handlers
        logging.root.setLevel(log_level)
        for handler in logging.root.handlers:
            handler.setFormatter(formatter)
        _logging_configured = True
        logging.info(f"Logging level updated to: {log_level}")
    else:
        # Already configured and logged, just update the level silently
        logging.root.setLevel(log_level)
