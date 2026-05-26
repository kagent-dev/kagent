import httpx
import pytest

from kagent.adk._token import KAgentTokenService


@pytest.mark.asyncio
async def test_add_headers_includes_a2a_version_and_identity(monkeypatch):
    service = KAgentTokenService("test-agent")
    service.token = "test-token"
    monkeypatch.setattr("kagent.adk._token.get_request_user_id", lambda: "user-1")

    request = httpx.Request("GET", "http://kagent.local/api/tasks")
    await service._add_headers(request)

    assert request.headers["A2A-Version"] == "1.0"
    assert request.headers["X-Agent-Name"] == "test-agent"
    assert request.headers["X-User-Id"] == "user-1"
    assert request.headers["Authorization"] == "Bearer test-token"
