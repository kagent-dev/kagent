"""Legacy session headers never supply credentials to a new kagent invocation."""

from types import SimpleNamespace
from unittest.mock import AsyncMock

import pytest
from kagent.core._runtime_config import TokenPropagationConfig

from kagent.adk._bearer_token import bearer_token
from kagent.adk._invocation_auth import InvocationCredentialError, request_headers
from kagent.adk._llm_passthrough_plugin import LLMPassthroughPlugin
from kagent.adk.cli import create_sts_integration


@pytest.fixture(autouse=True)
def invocation():
    headers = request_headers.set({})
    token = bearer_token.set(None)
    yield
    request_headers.reset(headers)
    bearer_token.reset(token)


class PassthroughModel:
    api_key_passthrough = True

    def __init__(self):
        self.tokens = []

    def set_passthrough_key(self, token):
        self.tokens.append(token)


def callback_context(model):
    return SimpleNamespace(
        state={"headers": {"Authorization": "Bearer legacy-saved-token"}},
        _invocation_context=SimpleNamespace(agent=SimpleNamespace(model=model)),
    )


async def test_llm_uses_current_callers_and_rejects_missing_credentials():
    plugin = LLMPassthroughPlugin()
    model = PassthroughModel()
    context = callback_context(model)
    for caller in ("human", "machine"):
        bearer_token.set(caller)
        await plugin.before_model_callback(callback_context=context, llm_request=None)
    bearer_token.set(None)
    with pytest.raises(InvocationCredentialError):
        await plugin.before_model_callback(callback_context=context, llm_request=None)
    assert model.tokens == ["human", "machine"]


async def test_llm_without_passthrough_needs_no_caller_credential():
    plugin = LLMPassthroughPlugin()
    model = PassthroughModel()
    model.api_key_passthrough = False
    await plugin.before_model_callback(callback_context=callback_context(model), llm_request=None)
    assert not model.tokens


@pytest.mark.parametrize("sts_enabled", [False, True])
async def test_cli_sts_plugin_uses_current_invocation_and_ignores_legacy_headers(sts_enabled):
    settings = TokenPropagationConfig(enabled=True)
    if sts_enabled:
        settings.sts_well_known_uri = "https://sts.example/.well-known/oauth-authorization-server"
    plugin = create_sts_integration(settings)
    assert plugin is not None
    if sts_enabled:
        plugin.sts_integration.exchange_token = AsyncMock(side_effect=lambda **kwargs: "sts-" + kwargs["subject_token"])
    session = SimpleNamespace(id="shared-session", state={"headers": {"Authorization": "Bearer legacy-saved-token"}})
    contexts = []
    for caller in ("human", "machine", None):
        bearer_token.set(caller)
        context = SimpleNamespace(session=session, invocation_id=str(caller))
        contexts.append(context)
        await plugin.before_run_callback(invocation_context=context)
        expected = ("sts-" if sts_enabled else "") + caller if caller else None
        assert plugin.header_provider(SimpleNamespace(_invocation_context=context)) == (
            {"Authorization": f"Bearer {expected}"} if expected else {}
        )
    # Resolving a later caller must not change an in-flight invocation's token.
    prefix = "sts-" if sts_enabled else ""
    assert plugin.header_provider(SimpleNamespace(_invocation_context=contexts[0])) == {
        "Authorization": f"Bearer {prefix}human"
    }
    if sts_enabled:
        assert [call.kwargs["subject_token"] for call in plugin.sts_integration.exchange_token.await_args_list] == [
            "human",
            "machine",
        ]
    assert session.state == {"headers": {"Authorization": "Bearer legacy-saved-token"}}
