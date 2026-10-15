import json
import logging

from opentelemetry.sdk.trace import TracerProvider

from kagent.core._logging import JsonFormatter


def _record(message="hello", exc_info=None):
    return logging.LogRecord("kagent.test", logging.ERROR, __file__, 1, message, None, exc_info)


def test_log_line_is_one_json_object_with_trace_context():
    tracer = TracerProvider().get_tracer("test")

    with tracer.start_as_current_span("turn") as span:
        line = JsonFormatter().format(_record())

    entry = json.loads(line)
    context = span.get_span_context()
    assert "\n" not in line
    assert entry["msg"] == "hello"
    assert entry["level"] == "ERROR"
    assert entry["trace_id"] == f"{context.trace_id:032x}"
    assert entry["span_id"] == f"{context.span_id:016x}"
    assert entry["trace_flags"] == f"{context.trace_flags:02x}"


def test_log_line_outside_a_span_has_no_trace_context():
    entry = json.loads(JsonFormatter().format(_record()))

    assert "trace_id" not in entry


def test_exception_stacktrace_stays_in_one_field():
    try:
        raise ValueError("boom")
    except ValueError:
        import sys

        line = JsonFormatter().format(_record(exc_info=sys.exc_info()))

    entry = json.loads(line)
    assert "\n" not in line
    assert entry["exception.type"] == "ValueError"
    assert "ValueError: boom" in entry["exception.stacktrace"]
