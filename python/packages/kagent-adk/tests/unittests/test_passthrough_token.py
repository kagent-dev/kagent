"""Resolution order for the credential sent on an outbound LLM call."""

import pytest

from kagent.adk._bearer_token import (
    bearer_token,
    resolve_passthrough_token,
    session_id,
    set_exchanged_token_provider,
)

INBOUND = "INBOUND-CALLER-TOKEN"
EXCHANGED = "EXCHANGED-STS-TOKEN"
SESSION = "session-1"


class FakeProvider:
    def __init__(self, by_session):
        self.by_session = by_session
        self.calls = 0

    def get_token_for_session(self, session_id: str):
        self.calls += 1
        return self.by_session.get(session_id)


class RaisingProvider:
    def get_token_for_session(self, session_id: str):
        raise RuntimeError("cache unavailable")


@pytest.fixture(autouse=True)
def reset_state():
    yield
    set_exchanged_token_provider(None)
    bearer_token.set(None)
    session_id.set(None)


def test_exchanged_token_wins_over_the_callers_own():
    set_exchanged_token_provider(FakeProvider({SESSION: EXCHANGED}))
    assert resolve_passthrough_token(inbound_token=INBOUND, current_session_id=SESSION) == EXCHANGED


def test_falls_back_when_no_token_is_cached_for_the_session():
    set_exchanged_token_provider(FakeProvider({}))
    assert resolve_passthrough_token(inbound_token=INBOUND, current_session_id=SESSION) == INBOUND


def test_falls_back_when_no_session_is_known():
    set_exchanged_token_provider(FakeProvider({SESSION: EXCHANGED}))
    assert resolve_passthrough_token(inbound_token=INBOUND, current_session_id=None) == INBOUND


def test_falls_back_when_no_provider_is_registered():
    assert resolve_passthrough_token(inbound_token=INBOUND, current_session_id=SESSION) == INBOUND


def test_falls_back_when_the_provider_raises():
    set_exchanged_token_provider(RaisingProvider())
    assert resolve_passthrough_token(inbound_token=INBOUND, current_session_id=SESSION) == INBOUND


def test_returns_none_when_neither_source_has_a_token():
    set_exchanged_token_provider(FakeProvider({}))
    assert resolve_passthrough_token(inbound_token=None, current_session_id=SESSION) is None


def test_reads_the_context_vars_when_no_arguments_are_given():
    """The embedding path has no callback_context, so it relies on the ContextVars."""
    set_exchanged_token_provider(FakeProvider({SESSION: EXCHANGED}))
    bearer_token.set(INBOUND)
    session_id.set(SESSION)
    assert resolve_passthrough_token() == EXCHANGED


def test_context_var_path_falls_back_to_the_inbound_token():
    set_exchanged_token_provider(FakeProvider({}))
    bearer_token.set(INBOUND)
    session_id.set(SESSION)
    assert resolve_passthrough_token() == INBOUND
