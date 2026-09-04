import grpc
import pytest
from a2a.types import AgentCard, a2a_pb2, a2a_pb2_grpc
from google.protobuf.json_format import ParseDict
from grpc_health.v1 import health_pb2, health_pb2_grpc
from kagent.core.a2a import KAgentGrpcServerCallContextBuilder

import kagent.adk._a2a as _a2a
from kagent.adk import KAgentApp


def make_app(address: str = "127.0.0.1:0"):
    card = ParseDict(
        {
            "name": "test-app",
            "description": "test agent",
            "version": "0.0.1",
            "supportedInterfaces": [{"url": "http://localhost:8080", "protocolBinding": "JSONRPC"}],
            "capabilities": {},
            "defaultInputModes": ["text/plain"],
            "defaultOutputModes": ["text/plain"],
            "skills": [],
        },
        AgentCard(),
    )
    return KAgentApp(
        root_agent_factory=lambda: None,
        agent_card=card,
        kagent_api_url="http://unused",
        app_name="test-app",
        a2a_grpc_address=address,
    ).build(local=True)


@pytest.mark.asyncio
async def test_grpc_health():
    app = make_app()

    async with app.router.lifespan_context(app):
        async with grpc.aio.insecure_channel(f"127.0.0.1:{app.state.a2a_grpc_port}") as channel:
            response = await health_pb2_grpc.HealthStub(channel).Check(
                health_pb2.HealthCheckRequest(service="lf.a2a.v1.A2AService")
            )

    assert response.status == health_pb2.HealthCheckResponse.SERVING


@pytest.mark.asyncio
async def test_grpc_dispatch_uses_jsonrpc_handler(monkeypatch):
    handlers = []
    real_create_jsonrpc_routes = _a2a.create_jsonrpc_routes
    real_grpc_handler = _a2a.GrpcHandler

    def capture_jsonrpc_handler(handler, *args, **kwargs):
        handlers.append(handler)
        return real_create_jsonrpc_routes(handler, *args, **kwargs)

    def capture_grpc_handler(handler, *args, **kwargs):
        handlers.append(handler)
        return real_grpc_handler(handler, *args, **kwargs)

    monkeypatch.setattr(_a2a, "create_jsonrpc_routes", capture_jsonrpc_handler)
    monkeypatch.setattr(_a2a, "GrpcHandler", capture_grpc_handler)
    app = make_app()

    async with app.router.lifespan_context(app):
        async with grpc.aio.insecure_channel(f"127.0.0.1:{app.state.a2a_grpc_port}") as channel:
            response = await a2a_pb2_grpc.A2AServiceStub(channel).ListTasks(a2a_pb2.ListTasksRequest())

    assert response.total_size == 0
    assert len(handlers) == 2
    assert handlers[0] is handlers[1]


def test_grpc_metadata_preserves_user_identity():
    class FakeContext:
        @staticmethod
        def invocation_metadata():
            return (("x-user-id", "alice"), ("x-kagent-source", "parent"))

    context = KAgentGrpcServerCallContextBuilder().build(FakeContext())

    assert context.user.user_name == "alice"
    assert context.state["headers"]["x-kagent-source"] == "parent"
    assert context.state["kagent_source"] == "parent"
