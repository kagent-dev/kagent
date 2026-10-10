import json
import logging
import os
from datetime import datetime, timezone

from opentelemetry import trace

_logging_configured = False


class JsonFormatter(logging.Formatter):
    """One JSON object per line, with the field names go/pkg/logging writes."""

    def format(self, record: logging.LogRecord) -> str:
        entry = {
            "time": datetime.fromtimestamp(record.created, tz=timezone.utc).isoformat(),
            "level": record.levelname,
            "logger": record.name,
            "msg": record.getMessage(),
        }
        span_context = trace.get_current_span().get_span_context()
        if span_context.is_valid:
            entry["trace_id"] = trace.format_trace_id(span_context.trace_id)
            entry["span_id"] = trace.format_span_id(span_context.span_id)
            entry["trace_flags"] = f"{span_context.trace_flags:02x}"
        if record.exc_info and record.exc_info[0] is not None:
            entry["exception.type"] = record.exc_info[0].__name__
            entry["exception.stacktrace"] = self.formatException(record.exc_info)
        elif record.stack_info:
            entry["exception.stacktrace"] = self.formatStack(record.stack_info)
        return json.dumps(entry, default=str)


def configure_logging() -> None:
    """Configure logging based on KAGENT_LOG_LEVEL environment variable."""
    global _logging_configured

    log_level = os.getenv("KAGENT_LOG_LEVEL", "INFO").upper()
    formatter = JsonFormatter()

    if not logging.root.handlers:
        handler = logging.StreamHandler()
        handler.setFormatter(formatter)
        logging.root.addHandler(handler)
        logging.root.setLevel(log_level)
        _logging_configured = True
        logging.info("Logging configured with level: %s", log_level)
    elif not _logging_configured:
        logging.root.setLevel(log_level)
        for handler in logging.root.handlers:
            handler.setFormatter(formatter)
        _logging_configured = True
        logging.info("Logging level updated to: %s", log_level)
    else:
        logging.root.setLevel(log_level)
