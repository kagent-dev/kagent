"""The passthrough plugin sets the credential the resolver chose on the model."""

from types import SimpleNamespace
from typing import Optional

import pytest

from kagent.adk._bearer_token import set_exchanged_token_provider
from kagent.adk._llm_passthrough_plugin import LLMPassthroughPlugin

INBOUND = "INBOUND-CALLER-TOKEN"
EXCHANGED = "EXCHANGED-STS-TOKEN"
SESSION = "session-1"


class FakeModel:
    def __init__(self, api_key_passthrough: bool = True):
        self.api_key_passthrough = api_key_passthrough
        self.key: Optional[str] = None

    def set_passthrough_key(self, token: str) -> None:
        self.key = token


class FakeProvider:
    def __init__(self, by_session):
        self.by_session = by_session

    def get_token_for_session(self, session_id: str):
        return self.by_session.get(session_id)


def callback_context(model: FakeModel, headers: dict):
    return SimpleNamespace(
        state={"headers": headers},
        _invocation_context=SimpleNamespace(
            agent=SimpleNamespace(model=model),
            session=SimpleNamespace(id=SESSION),
        ),
    )


@pytest.fixture(autouse=True)
def reset_provider():
    yield
    set_exchanged_token_provider(None)


@pytest.mark.asyncio
async def test_sets_the_exchanged_token_when_one_is_cached():
    set_exchanged_token_provider(FakeProvider({SESSION: EXCHANGED}))
    model = FakeModel()

    await LLMPassthroughPlugin().before_model_callback(
        callback_context=callback_context(model, {"authorization": f"Bearer {INBOUND}"}),
        llm_request=None,
    )

    assert model.key == EXCHANGED


@pytest.mark.asyncio
async def test_sets_the_exchanged_token_even_with_no_inbound_header():
    set_exchanged_token_provider(FakeProvider({SESSION: EXCHANGED}))
    model = FakeModel()

    await LLMPassthroughPlugin().before_model_callback(
        callback_context=callback_context(model, {}),
        llm_request=None,
    )

    assert model.key == EXCHANGED


@pytest.mark.asyncio
async def test_falls_back_to_the_inbound_token():
    set_exchanged_token_provider(FakeProvider({}))
    model = FakeModel()

    await LLMPassthroughPlugin().before_model_callback(
        callback_context=callback_context(model, {"authorization": f"Bearer {INBOUND}"}),
        llm_request=None,
    )

    assert model.key == INBOUND


@pytest.mark.asyncio
async def test_sets_nothing_when_passthrough_is_disabled_on_the_model():
    set_exchanged_token_provider(FakeProvider({SESSION: EXCHANGED}))
    model = FakeModel(api_key_passthrough=False)

    await LLMPassthroughPlugin().before_model_callback(
        callback_context=callback_context(model, {"authorization": f"Bearer {INBOUND}"}),
        llm_request=None,
    )

    assert model.key is None


@pytest.mark.asyncio
async def test_sets_nothing_when_no_token_is_available():
    model = FakeModel()

    await LLMPassthroughPlugin().before_model_callback(
        callback_context=callback_context(model, {}),
        llm_request=None,
    )

    assert model.key is None
