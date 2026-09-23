"""Resolution order for the credential sent on an outbound LLM call."""

from typing import Optional

import pytest

from kagent.adk._bearer_token import (
    bearer_token,
    resolve_passthrough_token,
    session_id,
)

INBOUND = "INBOUND-CALLER-TOKEN"
EXCHANGED = "EXCHANGED-STS-TOKEN"
SESSION = "session-1"


class FakeProvider:
    def __init__(self, by_session):
        self.by_session = by_session
        self.calls = 0
        self.seen_bearer: Optional[str] = None

    def exchanged_token(self, *, session_id: str, bearer_token: str) -> Optional[str]:
        self.calls += 1
        self.seen_bearer = bearer_token
        return self.by_session.get(session_id)


class RaisingProvider:
    def exchanged_token(self, *, session_id: str, bearer_token: str) -> Optional[str]:
        raise RuntimeError("cache unavailable")


@pytest.fixture(autouse=True)
def reset_state():
    yield
    bearer_token.set(None)
    session_id.set(None)


def test_exchanged_token_wins_over_the_callers_own():
    provider = FakeProvider({SESSION: EXCHANGED})
    assert resolve_passthrough_token(provider, inbound_token=INBOUND, session_id=SESSION) == EXCHANGED


def test_the_provider_is_given_the_requests_own_caller_token():
    provider = FakeProvider({SESSION: EXCHANGED})
    resolve_passthrough_token(provider, inbound_token=INBOUND, session_id=SESSION)
    assert provider.seen_bearer == INBOUND


def test_falls_back_when_no_token_is_cached_for_the_session():
    assert resolve_passthrough_token(FakeProvider({}), inbound_token=INBOUND, session_id=SESSION) == INBOUND


def test_falls_back_when_no_session_is_known():
    provider = FakeProvider({SESSION: EXCHANGED})
    assert resolve_passthrough_token(provider, inbound_token=INBOUND, session_id=None) == INBOUND


def test_falls_back_when_no_provider_is_passed():
    assert resolve_passthrough_token(None, inbound_token=INBOUND, session_id=SESSION) == INBOUND


def test_falls_back_when_the_provider_raises():
    assert resolve_passthrough_token(RaisingProvider(), inbound_token=INBOUND, session_id=SESSION) == INBOUND


def test_returns_none_when_neither_source_has_a_token():
    assert resolve_passthrough_token(FakeProvider({}), inbound_token=None, session_id=SESSION) is None


def test_returns_none_when_the_request_presented_no_caller_token():
    """A later turn on the same session, carrying no Authorization."""
    provider = FakeProvider({SESSION: EXCHANGED})
    assert resolve_passthrough_token(provider, inbound_token=None, session_id=SESSION) is None
    assert provider.calls == 0, "an absent caller token must not reach the cache at all"


def test_an_explicitly_absent_token_is_not_read_from_the_context_var():
    """Both inputs are required, so None means absent rather than omitted."""
    bearer_token.set(INBOUND)
    session_id.set(SESSION)
    provider = FakeProvider({SESSION: EXCHANGED})

    assert resolve_passthrough_token(provider, inbound_token=None, session_id=None) is None
