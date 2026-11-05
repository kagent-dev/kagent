from unittest.mock import MagicMock

import pytest
from a2a.types import Message, Role
from google.protobuf.json_format import ParseDict
from opentelemetry import baggage, trace
from opentelemetry import context as otel_context
from opentelemetry.baggage.propagation import W3CBaggagePropagator
from opentelemetry.processor.baggage import BaggageSpanProcessor
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter

from kagent.core.telemetry._context_attributes import (
    MAX_CONTEXT_KEYS,
    TRACE_CONTEXT_KEYS_ENV_VAR,
    _allowed_context_mappings,
    allowed_baggage_key_predicate,
    context_with_promoted_metadata,
    detach_promoted_metadata,
    promote_message_metadata_to_baggage,
)
from kagent.core.telemetry._span_processor import (
    KAGENT_ATTRIBUTES_KEY,
    KagentAttributesSpanProcessor,
)


def set_allowlist(monkeypatch, allowlist: str) -> None:
    if allowlist:
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, allowlist)
    else:
        monkeypatch.delenv(TRACE_CONTEXT_KEYS_ENV_VAR, raising=False)
    _allowed_context_mappings.cache_clear()


def baggage_context(members: dict[str, str]) -> otel_context.Context:
    context = otel_context.Context()
    for key, value in members.items():
        context = baggage.set_baggage(key, value, context)
    return context


def baggage_values(context: otel_context.Context) -> dict[str, str]:
    return {key: value for key, value in baggage.get_all(context).items()}


@pytest.fixture(autouse=True)
def _clear_allowlist_cache():
    _allowed_context_mappings.cache_clear()
    yield
    _allowed_context_mappings.cache_clear()


class TestContextWithPromotedMetadata:
    def test_empty_allowlist_is_a_noop(self, monkeypatch):
        set_allowlist(monkeypatch, "")
        ctx = baggage_context({"user.id": "opaque-subject"})
        assert baggage_values(context_with_promoted_metadata({"user.id": "from-metadata"}, ctx)) == {
            "user.id": "opaque-subject"
        }

    def test_leaves_existing_baggage_in_place(self, monkeypatch):
        set_allowlist(monkeypatch, "user.id")
        ctx = baggage_context({"user.id": "from-baggage"})
        assert baggage_values(context_with_promoted_metadata({"user.id": "from-metadata"}, ctx)) == {
            "user.id": "from-baggage"
        }

    def test_promotes_allowlisted_metadata(self, monkeypatch):
        set_allowlist(monkeypatch, "user.id,thread_id")
        assert baggage_values(
            context_with_promoted_metadata({"user.id": "opaque-subject", "thread_id": "T123", "secret": "nope"})
        ) == {"user.id": "opaque-subject", "thread_id": "T123"}

    def test_empty_metadata_does_not_wipe_baggage(self, monkeypatch):
        set_allowlist(monkeypatch, "user.id")
        ctx = baggage_context({"user.id": "from-baggage"})
        assert baggage_values(context_with_promoted_metadata({"user.id": "  "}, ctx)) == {"user.id": "from-baggage"}

    def test_remaps_baggage_from_onto_to(self, monkeypatch):
        set_allowlist(monkeypatch, '[{"from":"sub","to":"user.id"}]')
        ctx = baggage_context({"sub": "opaque-subject"})
        assert baggage_values(context_with_promoted_metadata({}, ctx)) == {
            "sub": "opaque-subject",
            "user.id": "opaque-subject",
        }

    def test_does_not_remap_over_existing_to(self, monkeypatch):
        set_allowlist(monkeypatch, '[{"from":"sub","to":"user.id"}]')
        ctx = baggage_context({"sub": "from-sub", "user.id": "already"})
        assert baggage_values(context_with_promoted_metadata({}, ctx)) == {
            "sub": "from-sub",
            "user.id": "already",
        }

    def test_maps_metadata_from_onto_to(self, monkeypatch):
        set_allowlist(monkeypatch, '[{"from":"sub","to":"user.id"},"gen_ai.conversation.id"]')
        assert baggage_values(
            context_with_promoted_metadata({"sub": "opaque-subject", "gen_ai.conversation.id": "sess-1"})
        ) == {"user.id": "opaque-subject", "gen_ai.conversation.id": "sess-1"}

    def test_skips_non_scalars(self, monkeypatch):
        set_allowlist(monkeypatch, "value")
        assert baggage_values(context_with_promoted_metadata({"value": {"nested": True}})) == {}

    @pytest.mark.parametrize(
        ("value", "expected"),
        [
            (True, "true"),
            (False, "false"),
            (3, "3"),
            (1.5, "1.5"),
        ],
    )
    def test_renders_scalar_metadata(self, monkeypatch, value, expected):
        set_allowlist(monkeypatch, "value")
        assert baggage_values(context_with_promoted_metadata({"value": value})) == {"value": expected}

    def test_strips_control_characters(self, monkeypatch):
        set_allowlist(monkeypatch, "note")
        assert baggage_values(context_with_promoted_metadata({"note": "line\nbreak\tand\x00nul"})) == {
            "note": "linebreakandnul"
        }

    def test_drops_keys_with_whitespace(self, monkeypatch):
        set_allowlist(monkeypatch, "bad key,user.id")
        assert baggage_values(context_with_promoted_metadata({"bad key": "x", "user.id": "ok"})) == {"user.id": "ok"}

    def test_rejects_leftover_hmac_sha256_hash_mappings(self, monkeypatch):
        set_allowlist(monkeypatch, '[{"from":"email","to":"user.hash","hash":"hmac-sha256"}]')
        assert baggage_values(context_with_promoted_metadata({"email": "person@example.test"})) == {}

    def test_does_not_remap_leftover_hashed_mappings_from_baggage(self, monkeypatch):
        set_allowlist(monkeypatch, '[{"from":"email","to":"user.hash","hash":"hmac-sha256"}]')
        ctx = baggage_context({"email": "person@example.test"})
        assert baggage_values(context_with_promoted_metadata({}, ctx)) == {"email": "person@example.test"}

    def test_rejects_unknown_leftover_hash_mappings(self, monkeypatch):
        set_allowlist(monkeypatch, '[{"from":"email","to":"user.hash","hash":"sha3"}]')
        assert baggage_values(context_with_promoted_metadata({"email": "person@example.test"})) == {}

    def test_keeps_mappings_without_hash_when_hashed_entry_dropped(self, monkeypatch):
        set_allowlist(
            monkeypatch,
            '[{"from":"email","to":"user.hash","hash":"hmac-sha256"},{"from":"sub","to":"user.id"}]',
        )
        assert baggage_values(
            context_with_promoted_metadata({"email": "person@example.test", "sub": "opaque-subject"})
        ) == {"user.id": "opaque-subject"}

    def test_caps_the_allowlist(self, monkeypatch):
        keys = [f"k{i}" for i in range(MAX_CONTEXT_KEYS + 4)]
        set_allowlist(monkeypatch, ",".join(keys))
        got = baggage_values(context_with_promoted_metadata({key: "v" for key in keys}))
        assert len(got) == MAX_CONTEXT_KEYS

    def test_skips_message_decode_when_allowlist_empty(self, monkeypatch):
        set_allowlist(monkeypatch, "")
        decode = MagicMock(return_value={"thread_id": "T1"})
        monkeypatch.setattr("kagent.core.telemetry._context_attributes.MessageToDict", decode)
        assert baggage_values(context_with_promoted_metadata(message=MagicMock())) == {}
        decode.assert_not_called()

    def test_decodes_message_after_allowlist_check(self, monkeypatch):
        set_allowlist(monkeypatch, "thread_id")
        message = MagicMock()
        monkeypatch.setattr(
            "kagent.core.telemetry._context_attributes.MessageToDict",
            lambda _message: {"thread_id": "T1"},
        )
        assert baggage_values(context_with_promoted_metadata(message=message)) == {"thread_id": "T1"}


def test_allowed_baggage_key_predicate(monkeypatch):
    set_allowlist(monkeypatch, "")
    assert allowed_baggage_key_predicate() is None

    set_allowlist(monkeypatch, '[{"from":"sub","to":"user.id"},"thread_id"]')
    predicate = allowed_baggage_key_predicate()
    assert predicate is not None
    assert predicate("user.id")
    assert predicate("thread_id")
    assert not predicate("sub")
    assert not predicate("secret")


def test_promote_attaches_and_detaches(monkeypatch):
    set_allowlist(monkeypatch, "user.id")
    token = promote_message_metadata_to_baggage(metadata={"user.id": "opaque-subject"})
    assert baggage.get_baggage("user.id") == "opaque-subject"
    detach_promoted_metadata(token)
    assert baggage.get_baggage("user.id") is None


def _span_attrs(spans, name: str) -> dict:
    for span in spans:
        if span.name == name:
            return dict(span.attributes or {})
    raise AssertionError(f"span {name} not found")


def test_caller_context_lands_on_every_span(monkeypatch):
    set_allowlist(monkeypatch, "user.id")
    exporter = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    provider.add_span_processor(BaggageSpanProcessor(allowed_baggage_key_predicate()))
    provider.add_span_processor(KagentAttributesSpanProcessor())

    token = promote_message_metadata_to_baggage(
        metadata=None,
        context=baggage_context({"user.id": "opaque-subject"}),
    )
    attrs_token = otel_context.attach(otel_context.set_value(KAGENT_ATTRIBUTES_KEY, {"kagent.user_id": "runtime-user"}))
    try:
        tracer = provider.get_tracer("test")
        with tracer.start_as_current_span("invocation"):
            with tracer.start_as_current_span("tool"):
                with tracer.start_as_current_span("generate_content"):
                    pass
    finally:
        otel_context.detach(attrs_token)
        detach_promoted_metadata(token)
        provider.shutdown()

    for name in ("invocation", "tool", "generate_content"):
        attrs = _span_attrs(exporter.get_finished_spans(), name)
        assert attrs["user.id"] == "opaque-subject"
        assert attrs["kagent.user_id"] == "runtime-user"


def test_runtime_attributes_win_over_allowlisted_baggage(monkeypatch):
    set_allowlist(monkeypatch, "gen_ai.conversation.id")
    exporter = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    provider.add_span_processor(BaggageSpanProcessor(allowed_baggage_key_predicate()))
    provider.add_span_processor(KagentAttributesSpanProcessor())

    token = otel_context.attach(baggage_context({"gen_ai.conversation.id": "from-caller"}))
    attrs_token = otel_context.attach(
        otel_context.set_value(KAGENT_ATTRIBUTES_KEY, {"gen_ai.conversation.id": "from-runtime"})
    )
    try:
        tracer = provider.get_tracer("test")
        with tracer.start_as_current_span("generate_content"):
            pass
    finally:
        otel_context.detach(attrs_token)
        otel_context.detach(token)
        provider.shutdown()

    attrs = _span_attrs(exporter.get_finished_spans(), "generate_content")
    assert attrs["gen_ai.conversation.id"] == "from-runtime"


def test_promotes_real_protobuf_message_metadata(monkeypatch):
    set_allowlist(monkeypatch, '[{"from":"sub","to":"user.id"},"attempt"]')
    message = Message(role=Role.ROLE_USER, message_id="msg-1")
    ParseDict({"sub": "opaque-subject", "attempt": 3, "private_token": "must-not-be-exported"}, message.metadata)
    assert baggage_values(context_with_promoted_metadata(message=message)) == {
        "user.id": "opaque-subject",
        "attempt": "3",
    }


@pytest.mark.parametrize("value", ["case with spaces", "café", "literal%2Fid", "literal%252Fid", "100%", "a,b;c=d"])
@pytest.mark.parametrize("source", ["metadata", "mapped metadata", "baggage"])
def test_promoted_context_preserves_raw_values(monkeypatch, value, source):
    policy = "thread_id" if source == "metadata" else '[{"from":"source","to":"thread_id"}]'
    key = "thread_id" if source == "metadata" else "source"
    set_allowlist(monkeypatch, policy)
    ctx = otel_context.Context()
    metadata = {key: value, "private_token": "must-not-be-exported"}
    if source == "baggage":
        ctx = baggage_context({key: value})
        metadata = None
    ctx = context_with_promoted_metadata(metadata, ctx)
    assert baggage.get_baggage("thread_id", ctx) == value

    exporter = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(BaggageSpanProcessor(allowed_baggage_key_predicate()))
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    try:
        tracer = provider.get_tracer("test")
        root = tracer.start_span("invocation", context=ctx)
        child_ctx = trace.set_span_in_context(root, ctx)
        model = tracer.start_span("model", context=child_ctx)
        model.end()
        root.end()
        for name in ("invocation", "model"):
            attrs = _span_attrs(exporter.get_finished_spans(), name)
            assert attrs["thread_id"] == value
            assert "private_token" not in attrs

        carrier = {}
        propagator = W3CBaggagePropagator()
        propagator.inject(carrier, context=ctx)
        downstream = propagator.extract(carrier, context=otel_context.Context())
        assert baggage.get_baggage("thread_id", downstream) == value
    finally:
        provider.shutdown()
