"""The host application owns the current caller credential source."""

from types import SimpleNamespace
from unittest.mock import AsyncMock, Mock

import pytest

from agentsts.adk import ADKSTSIntegration, ADKTokenPropagationPlugin


@pytest.mark.parametrize("asynchronous", [False, True])
async def test_current_caller_partitions_an_explicit_subject_without_saved_headers(asynchronous):
    current = {"caller": "human"}

    def caller():
        return current["caller"]

    async def async_caller():
        return caller()

    sts = Mock(spec=ADKSTSIntegration)
    sts.get_subject_token = Mock(side_effect=lambda state: state["delegated-subject"])
    sts.fetch_actor_token = None
    sts._actor_token = "actor"
    sts.exchange_token = AsyncMock(side_effect=["exchanged-human", "exchanged-machine"])
    plugin = ADKTokenPropagationPlugin(sts, get_caller_token=async_caller if asynchronous else caller)
    session = SimpleNamespace(
        id="shared-session",
        state={"headers": {"Authorization": "Bearer stale-caller"}, "delegated-subject": "configured-subject"},
    )
    contexts = []
    for credential in ("human", "machine"):
        current["caller"] = credential
        context = SimpleNamespace(invocation_id=credential, session=session)
        contexts.append(context)
        await plugin.before_run_callback(invocation_context=context)
        assert plugin.header_provider(SimpleNamespace(_invocation_context=context)) == {
            "Authorization": f"Bearer exchanged-{credential}"
        }
        assert plugin.cache_key(context) is None
        assert plugin.cache_key(context, caller_token=credential) in plugin.token_cache
    assert len(plugin.token_cache) == 2
    assert [call.kwargs["subject_token"] for call in sts.exchange_token.await_args_list] == [
        "configured-subject",
        "configured-subject",
    ]
    assert plugin.header_provider(SimpleNamespace(_invocation_context=contexts[0])) == {
        "Authorization": "Bearer exchanged-human"
    }


@pytest.mark.parametrize("failure", [None, RuntimeError("resolution failed")])
async def test_failed_caller_resolution_never_falls_back_to_saved_or_explicit_subject(failure):
    resolver = Mock(side_effect=["human", failure])
    sts = Mock(spec=ADKSTSIntegration)
    sts.get_subject_token = Mock(return_value="configured-subject")
    sts.fetch_actor_token = None
    sts._actor_token = None
    sts.exchange_token = AsyncMock(return_value="exchanged")
    plugin = ADKTokenPropagationPlugin(sts, get_caller_token=resolver)
    session = SimpleNamespace(id="shared", state={"headers": {"Authorization": "Bearer saved-caller"}})
    for invocation_id in ("first", "refused"):
        context = SimpleNamespace(invocation_id=invocation_id, session=session)
        await plugin.before_run_callback(invocation_context=context)
    assert plugin.header_provider(SimpleNamespace(_invocation_context=context)) == {}
    sts.get_subject_token.assert_called_once_with(session.state)
    sts.exchange_token.assert_awaited_once()
