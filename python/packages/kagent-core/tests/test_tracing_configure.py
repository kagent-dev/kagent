from types import SimpleNamespace

from kagent.core.tracing import _utils


def test_configure_tracing_logging_enabled_uses_event_logger_provider(monkeypatch):
    monkeypatch.setenv("OTEL_LOGGING_ENABLED", "true")
    monkeypatch.setenv("OTEL_TRACING_ENABLED", "false")

    instrument_calls = {}

    class FakeOpenAIInstrumentor:
        def __init__(self, **kwargs):
            instrument_calls["init_kwargs"] = kwargs

        def instrument(self, **kwargs):
            instrument_calls["instrument_kwargs"] = kwargs

    class FakeLogRecordProcessor:
        def shutdown(self) -> None:
            instrument_calls["log_processor_shutdown"] = True

    def fake_event_logger_provider(logger_provider):
        event_logger_provider = {"logger_provider": logger_provider}
        instrument_calls["event_logger_provider"] = event_logger_provider
        return event_logger_provider

    def fake_instrument_anthropic(event_logger_provider=None):
        instrument_calls["anthropic_event_logger_provider"] = event_logger_provider

    monkeypatch.setattr(_utils, "OpenAIInstrumentor", FakeOpenAIInstrumentor)
    monkeypatch.setattr(_utils, "EventLoggerProvider", fake_event_logger_provider)
    monkeypatch.setattr(_utils, "OTLPLogExporter", lambda *args, **kwargs: object())
    monkeypatch.setattr(_utils, "BatchLogRecordProcessor", lambda *args, **kwargs: FakeLogRecordProcessor())
    monkeypatch.setattr(_utils, "_instrument_anthropic", fake_instrument_anthropic)
    monkeypatch.setattr(_utils, "_instrument_google_generativeai", lambda: None)
    monkeypatch.setattr(
        _utils,
        "_logs",
        SimpleNamespace(set_logger_provider=lambda provider: instrument_calls.setdefault("logger_provider", provider)),
    )

    _utils.configure(name="test", namespace="test")

    assert instrument_calls["init_kwargs"] == {"use_legacy_attributes": False}
    assert "event_logger_provider" in instrument_calls["instrument_kwargs"]
    assert instrument_calls["anthropic_event_logger_provider"] == instrument_calls["event_logger_provider"]


def test_configure_tracing_logging_disabled_uses_legacy_instrumentation(monkeypatch):
    monkeypatch.setenv("OTEL_LOGGING_ENABLED", "false")
    monkeypatch.setenv("OTEL_TRACING_ENABLED", "false")

    instrument_calls = {}

    class FakeOpenAIInstrumentor:
        def __init__(self, **kwargs):
            instrument_calls["init_kwargs"] = kwargs

        def instrument(self, **kwargs):
            instrument_calls["instrument_kwargs"] = kwargs

    def fake_instrument_anthropic(event_logger_provider=None):
        instrument_calls["anthropic_event_logger_provider"] = event_logger_provider

    def fake_instrument_google():
        instrument_calls["google_instrumented"] = True

    monkeypatch.setattr(_utils, "OpenAIInstrumentor", FakeOpenAIInstrumentor)
    monkeypatch.setattr(_utils, "_instrument_anthropic", fake_instrument_anthropic)
    monkeypatch.setattr(_utils, "_instrument_google_generativeai", fake_instrument_google)

    _utils.configure(name="test", namespace="test")

    assert instrument_calls["init_kwargs"] == {}
    assert instrument_calls["instrument_kwargs"] == {}
    assert instrument_calls["anthropic_event_logger_provider"] is None
    assert instrument_calls["google_instrumented"] is True
