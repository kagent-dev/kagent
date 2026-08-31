import hashlib
import hmac
from unittest.mock import MagicMock, patch

import pytest
from opentelemetry import baggage
from opentelemetry import context as otel_context
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter

from kagent.core.tracing import caller_context_attributes, merge_caller_context_attributes
from kagent.core.tracing._context_attributes import (
    CONTEXT_ATTRIBUTE_PREFIX,
    MAX_CONTEXT_KEY_LENGTH,
    MAX_CONTEXT_KEYS,
    MAX_CONTEXT_VALUE_LENGTH,
    TRACE_CONTEXT_HASH_KEY_ENV_VAR,
    TRACE_CONTEXT_KEYS_ENV_VAR,
    _allowed_context_mappings,
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


def hmac_sha256_hex(key: str, value: str) -> str:
    return hmac.new(key.encode("utf-8"), value.encode("utf-8"), hashlib.sha256).hexdigest()


class TestCallerContextAttributes:
    """Tests for allowlist-driven promotion of caller context onto spans."""

    def test_empty_allowlist_disables_promotion(self, monkeypatch):
        monkeypatch.delenv(TRACE_CONTEXT_KEYS_ENV_VAR, raising=False)
        assert (
            caller_context_attributes(
                {"thread_id": "T1"},
                baggage_context({"sub": "opaque-subject"}),
            )
            == {}
        )

    def test_promotes_allowlisted_baggage(self, monkeypatch):
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, "sub,thread_id")
        assert caller_context_attributes(None, baggage_context({"sub": "opaque-subject", "thread_id": "T123"})) == {
            "kagent.context.sub": "opaque-subject",
            "kagent.context.thread_id": "T123",
        }

    def test_promotes_allowlisted_message_metadata(self, monkeypatch):
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, "thread_id,channel")
        assert caller_context_attributes({"thread_id": "1717171.4242", "channel": "C0AB1"}) == {
            "kagent.context.thread_id": "1717171.4242",
            "kagent.context.channel": "C0AB1",
        }

    def test_message_metadata_overrides_baggage(self, monkeypatch):
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, "sub")
        assert caller_context_attributes(
            {"sub": "from-metadata"},
            baggage_context({"sub": "from-baggage"}),
        ) == {"kagent.context.sub": "from-metadata"}

    def test_empty_metadata_does_not_override_baggage(self, monkeypatch):
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, "sub")
        assert caller_context_attributes(
            {"sub": "   "},
            baggage_context({"sub": "from-baggage"}),
        ) == {"kagent.context.sub": "from-baggage"}

    def test_ignores_keys_outside_the_allowlist(self, monkeypatch):
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, "thread_id")
        assert caller_context_attributes(
            {"thread_id": "T1", "extra": "nope"},
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
            (1e20, "100000000000000000000"),
            (1e-20, "0.00000000000000000001"),
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
        """Custom keys still cannot replace service.name. Registry names are the exception."""
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, "service.name")
        promoted = caller_context_attributes({"service.name": "impostor"})
        assert "service.name" not in promoted
        assert promoted == {f"{CONTEXT_ATTRIBUTE_PREFIX}service.name": "impostor"}

    def test_registry_attributes_stay_unprefixed(self, monkeypatch):
        """Only the explicit registry set is left unprefixed."""
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, "user.id,user.hash,enduser.id,session.id,channel")
        assert caller_context_attributes(
            {
                "user.id": "opaque-subject",
                "user.hash": "abc",
                "enduser.id": "end-user",
                "session.id": "sess-1",
                "channel": "C0AB1",
            }
        ) == {
            "user.id": "opaque-subject",
            "user.hash": "abc",
            "enduser.id": "end-user",
            "session.id": "sess-1",
            "kagent.context.channel": "C0AB1",
        }

    def test_unknown_user_attributes_are_prefixed(self, monkeypatch):
        monkeypatch.setenv(
            TRACE_CONTEXT_KEYS_ENV_VAR,
            '[{"from":"x","to":"user.asdasd"},{"from":"y","to":"user.email"}]',
        )
        assert caller_context_attributes({"x": "nope", "y": "also-nope"}) == {
            "kagent.context.user.asdasd": "nope",
            "kagent.context.user.email": "also-nope",
        }

    def test_session_id_is_unprefixed_but_session_foo_is_not(self, monkeypatch):
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, "session.id,session.foo")
        assert caller_context_attributes({"session.id": "sess-1", "session.foo": "other"}) == {
            "session.id": "sess-1",
            "kagent.context.session.foo": "other",
        }

    def test_maps_source_keys_onto_registry_names(self, monkeypatch):
        monkeypatch.setenv(
            TRACE_CONTEXT_KEYS_ENV_VAR,
            '[{"from":"sub","to":"user.id"},{"from":"thread_id","to":"session.id"},"channel"]',
        )
        assert caller_context_attributes({"sub": "opaque-subject", "thread_id": "T123", "channel": "C0AB1"}) == {
            "user.id": "opaque-subject",
            "session.id": "T123",
            "kagent.context.channel": "C0AB1",
        }

    def test_kagent_destination_names_are_prefixed(self, monkeypatch):
        monkeypatch.setenv(
            TRACE_CONTEXT_KEYS_ENV_VAR,
            '[{"from":"uid","to":"kagent.user_id"},{"from":"tid","to":"kagent.thread_id"}]',
        )
        assert caller_context_attributes({"uid": "attacker", "tid": "T123"}) == {
            "kagent.context.kagent.user_id": "attacker",
            "kagent.context.kagent.thread_id": "T123",
        }

    def test_invalid_json_allowlist_promotes_nothing(self, monkeypatch):
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, '[{"from":"sub"')
        assert caller_context_attributes({"sub": "opaque-subject"}) == {}

    def test_hashes_with_hmac_sha256(self, monkeypatch):
        key = "test-hmac-key"
        monkeypatch.setenv(
            TRACE_CONTEXT_KEYS_ENV_VAR,
            '[{"from":"email","to":"user.hash","hash":"hmac-sha256"}]',
        )
        monkeypatch.setenv(TRACE_CONTEXT_HASH_KEY_ENV_VAR, key)
        promoted = caller_context_attributes({"email": "ada@example.com"})
        assert promoted == {"user.hash": hmac_sha256_hex(key, "ada@example.com")}
        assert not any("@example.com" in value for value in promoted.values())

    def test_hash_without_key_emits_nothing(self, monkeypatch):
        """Missing HMAC key must not fall back to putting the original value on the span."""
        monkeypatch.setenv(
            TRACE_CONTEXT_KEYS_ENV_VAR,
            '[{"from":"email","to":"user.hash","hash":"hmac-sha256"}]',
        )
        monkeypatch.delenv(TRACE_CONTEXT_HASH_KEY_ENV_VAR, raising=False)
        assert caller_context_attributes({"email": "ada@example.com"}) == {}

    def test_unknown_hash_emits_nothing(self, monkeypatch):
        monkeypatch.setenv(
            TRACE_CONTEXT_KEYS_ENV_VAR,
            '[{"from":"email","to":"user.hash","hash":"md5"}]',
        )
        monkeypatch.setenv(TRACE_CONTEXT_HASH_KEY_ENV_VAR, "test-hmac-key")
        assert caller_context_attributes({"email": "ada@example.com"}) == {}

    def test_non_string_hash_does_not_emit_plaintext(self, monkeypatch):
        """A numeric hash field must drop the mapping, not skip hashing."""
        monkeypatch.setenv(
            TRACE_CONTEXT_KEYS_ENV_VAR,
            '[{"from":"email","to":"user.hash","hash":123}]',
        )
        monkeypatch.setenv(TRACE_CONTEXT_HASH_KEY_ENV_VAR, "test-hmac-key")
        promoted = caller_context_attributes({"email": "ada@example.com"})
        assert promoted == {}
        assert not any("ada@example.com" in value for value in promoted.values())

    def test_non_string_to_drops_the_mapping(self, monkeypatch):
        monkeypatch.setenv(
            TRACE_CONTEXT_KEYS_ENV_VAR,
            '[{"from":"thread_id","to":123}]',
        )
        assert caller_context_attributes({"thread_id": "T1"}) == {}

    def test_skips_message_decode_when_allowlist_empty(self, monkeypatch):
        monkeypatch.delenv(TRACE_CONTEXT_KEYS_ENV_VAR, raising=False)
        with patch(
            "kagent.core.tracing._context_attributes.read_message_metadata",
            return_value={"thread_id": "T1"},
        ) as decode:
            assert caller_context_attributes(message=MagicMock()) == {}
            decode.assert_not_called()

    def test_decodes_message_when_allowlist_is_set(self, monkeypatch):
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, "thread_id")
        message = MagicMock()
        with patch(
            "kagent.core.tracing._context_attributes.read_message_metadata",
            return_value={"thread_id": "T1"},
        ) as decode:
            assert caller_context_attributes(message=message) == {"kagent.context.thread_id": "T1"}
            decode.assert_called_once_with(message)


class TestMergeCallerContextAttributes:
    def test_does_not_override_existing_keys(self, monkeypatch):
        monkeypatch.setenv(
            TRACE_CONTEXT_KEYS_ENV_VAR,
            '[{"from":"sub","to":"user.id"},"thread_id"]',
        )
        existing = {"user.id": "runtime-user", "kagent.user_id": "runtime-user"}
        merge_caller_context_attributes(
            existing,
            {"sub": "attacker", "thread_id": "T1"},
        )
        assert existing == {
            "user.id": "runtime-user",
            "kagent.user_id": "runtime-user",
            "kagent.context.thread_id": "T1",
        }


class TestAllowedContextMappings:
    """Tests for allowlist parsing and its bounds."""

    def test_caps_list_length(self, monkeypatch):
        keys = ",".join(f"key{index}" for index in range(MAX_CONTEXT_KEYS * 2))
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, keys)
        assert len(_allowed_context_mappings()) == MAX_CONTEXT_KEYS

    def test_drops_over_long_and_duplicate_keys(self, monkeypatch):
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, f"a,a,{'b' * (MAX_CONTEXT_KEY_LENGTH + 1)},c")
        assert [mapping.source for mapping in _allowed_context_mappings()] == ["a", "c"]

    def test_counts_key_length_in_code_points(self, monkeypatch):
        kept = "键" * 40
        dropped = "键" * 65
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, f"{kept},{dropped}")
        assert [mapping.source for mapping in _allowed_context_mappings()] == [kept]

    def test_drops_keys_that_are_not_valid_attribute_names(self, monkeypatch):
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, "good, bad key ,\tanother\tbad")
        assert [mapping.source for mapping in _allowed_context_mappings()] == ["good"]

    def test_empty_allowlist_disables_promotion(self, monkeypatch):
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, "   ,  ,")
        assert _allowed_context_mappings() == ()

    def test_cache_survives_env_change_until_cleared(self, monkeypatch):
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, "a")
        assert [mapping.source for mapping in _allowed_context_mappings()] == ["a"]
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, "b")
        assert [mapping.source for mapping in _allowed_context_mappings()] == ["a"]
        _allowed_context_mappings.cache_clear()
        assert [mapping.source for mapping in _allowed_context_mappings()] == ["b"]


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
        monkeypatch.setenv(TRACE_CONTEXT_KEYS_ENV_VAR, '[{"from":"sub","to":"user.id"}]')
        promoted = caller_context_attributes(None, baggage_context({"sub": "opaque-subject"}))

        spans = self.record_spans(promoted)

        assert set(spans) == {"root", "execute_tool", "generate_content"}
        for attributes in spans.values():
            assert attributes["user.id"] == "opaque-subject"

    def test_flag_off_leaves_spans_unchanged(self, monkeypatch):
        monkeypatch.delenv(TRACE_CONTEXT_KEYS_ENV_VAR, raising=False)
        promoted = caller_context_attributes({"thread_id": "T1"}, baggage_context({"sub": "opaque-subject"}))

        spans = self.record_spans(promoted)

        for attributes in spans.values():
            assert "user.id" not in attributes
            assert not [key for key in attributes if key.startswith(CONTEXT_ATTRIBUTE_PREFIX)]
