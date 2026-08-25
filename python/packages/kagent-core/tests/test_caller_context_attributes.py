import pytest
from opentelemetry import baggage
from opentelemetry import context as otel_context
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter

from kagent.core.tracing import caller_context_attributes
from kagent.core.tracing._context_attributes import (
    CONTEXT_ATTRIBUTE_PREFIX,
    MAX_CONTEXT_KEY_LENGTH,
    MAX_CONTEXT_KEYS,
    MAX_CONTEXT_VALUE_LENGTH,
    TRACE_CONTEXT_KEYS_ENV_VAR,
    _allowed_context_keys,
)
from kagent.core.tracing._span_processor import (
    KagentAttributesSpanProcessor,
    clear_kagent_span_attributes,
    set_kagent_span_attributes,
)


def baggage_context(members: dict[str, str]) -> otel_context.Context:
    context = otel_context.Context()
    for key, value in members.items():
        context = baggage.set_baggage(key, value, context)
    return context


class TestCallerContextAttributes:
    """Tests for allowlist-driven promotion of caller context onto spans."""

    def test_disabled_by_default(self, monkeypatch):
        monkeypatch.delenv(TRACE_CONTEXT_KEYS_ENV_VAR, raising=False)
        assert (
            caller_context_attributes(
                {"thread_id": "T1"},
                baggage_context({"user.email": "ada@example.com"}),
            )
            == {}
        )

    def test_promotes_allowlisted_baggage(self, monkeypatch):
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, "user.email,user.name")
        assert caller_context_attributes(
            None, baggage_context({"user.email": "ada@example.com", "user.name": "Ada"})
        ) == {
            "kagent.context.user.email": "ada@example.com",
            "kagent.context.user.name": "Ada",
        }

    def test_promotes_allowlisted_message_metadata(self, monkeypatch):
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, "thread_id,channel")
        assert caller_context_attributes({"thread_id": "1717171.4242", "channel": "C0AB1"}) == {
            "kagent.context.thread_id": "1717171.4242",
            "kagent.context.channel": "C0AB1",
        }

    def test_message_metadata_overrides_baggage(self, monkeypatch):
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, "user.email")
        assert caller_context_attributes(
            {"user.email": "from-metadata@example.com"},
            baggage_context({"user.email": "from-baggage@example.com"}),
        ) == {"kagent.context.user.email": "from-metadata@example.com"}

    def test_ignores_keys_outside_the_allowlist(self, monkeypatch):
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, "thread_id")
        assert caller_context_attributes(
            {"thread_id": "T1", "customer.pan": "4111111111111111"},
            baggage_context({"secret.token": "s3cret"}),
        ) == {"kagent.context.thread_id": "T1"}

    @pytest.mark.parametrize(
        ("value", "expected"),
        [
            (True, "true"),
            (False, "false"),
            (7, "7"),
            # protobuf Struct has one numeric type, so JSON integers arrive as floats.
            (3.0, "3"),
            (2.5, "2.5"),
        ],
    )
    def test_renders_scalar_metadata_types(self, monkeypatch, value, expected):
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, "value")
        assert caller_context_attributes({"value": value}) == {"kagent.context.value": expected}

    @pytest.mark.parametrize("value", [{"a": "b"}, ["a"], "", None])
    def test_skips_non_scalar_and_empty_metadata_values(self, monkeypatch, value):
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, "value")
        assert caller_context_attributes({"value": value}) == {}

    def test_strips_control_characters(self, monkeypatch):
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, "note")
        assert caller_context_attributes({"note": "line\nbreak\tand\x00nul"}) == {
            "kagent.context.note": "linebreakandnul"
        }

    def test_truncates_long_values(self, monkeypatch):
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, "note")
        promoted = caller_context_attributes({"note": "a" * (MAX_CONTEXT_VALUE_LENGTH * 2)})
        assert len(promoted["kagent.context.note"]) == MAX_CONTEXT_VALUE_LENGTH

    def test_cannot_shadow_semantic_conventions(self, monkeypatch):
        """The prefix is what stops caller data replacing service.name and friends."""
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, "service.name")
        promoted = caller_context_attributes({"service.name": "impostor"})
        assert "service.name" not in promoted
        assert promoted == {f"{CONTEXT_ATTRIBUTE_PREFIX}service.name": "impostor"}


class TestAllowedContextKeys:
    """Tests for allowlist parsing and its bounds."""

    def test_caps_list_length(self, monkeypatch):
        keys = ",".join(f"key{index}" for index in range(MAX_CONTEXT_KEYS * 2))
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, keys)
        assert len(_allowed_context_keys()) == MAX_CONTEXT_KEYS

    def test_drops_over_long_and_duplicate_keys(self, monkeypatch):
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, f"a,a,{'b' * (MAX_CONTEXT_KEY_LENGTH + 1)},c")
        assert _allowed_context_keys() == ["a", "c"]

    def test_drops_keys_that_are_not_valid_attribute_names(self, monkeypatch):
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, "good, bad key ,\tanother\tbad")
        assert _allowed_context_keys() == ["good"]

    def test_empty_allowlist_disables_promotion(self, monkeypatch):
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, "   ,  ,")
        assert _allowed_context_keys() == []


class TestPromotedAttributesReachEverySpan:
    """Langfuse and comparable backends filter on attributes present on each
    span, so promoted values must reach descendants, not just the root span."""

    @staticmethod
    def record_spans(span_attributes: dict) -> dict[str, dict]:
        exporter = InMemorySpanExporter()
        provider = TracerProvider()
        provider.add_span_processor(SimpleSpanProcessor(exporter))
        provider.add_span_processor(KagentAttributesSpanProcessor())
        tracer = provider.get_tracer("test")

        token = set_kagent_span_attributes(span_attributes)
        try:
            with tracer.start_as_current_span("root"):
                with tracer.start_as_current_span("execute_tool"):
                    with tracer.start_as_current_span("generate_content"):
                        pass
        finally:
            clear_kagent_span_attributes(token)
            provider.shutdown()

        return {span.name: dict(span.attributes or {}) for span in exporter.get_finished_spans()}

    def test_flag_on_stamps_every_span(self, monkeypatch):
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, "user.email")
        promoted = caller_context_attributes(None, baggage_context({"user.email": "ada@example.com"}))

        spans = self.record_spans(promoted)

        assert set(spans) == {"root", "execute_tool", "generate_content"}
        for attributes in spans.values():
            assert attributes["kagent.context.user.email"] == "ada@example.com"

    def test_flag_off_leaves_spans_unchanged(self, monkeypatch):
        monkeypatch.delenv(TRACE_CONTEXT_KEYS_ENV_VAR, raising=False)
        promoted = caller_context_attributes({"thread_id": "T1"}, baggage_context({"user.email": "ada@example.com"}))

        spans = self.record_spans(promoted)

        for attributes in spans.values():
            assert not [key for key in attributes if key.startswith(CONTEXT_ATTRIBUTE_PREFIX)]
