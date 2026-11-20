import asyncio
import json
import sys
from contextlib import asynccontextmanager

import grpc
import httpx
import pytest
from a2a.types import AgentCard, a2a_pb2, a2a_pb2_grpc
from google.protobuf.json_format import ParseDict
from grpc_health.v1 import health_pb2, health_pb2_grpc
from kagent.core import AsyncControllerClient
from kagent.core._runtime_config import RuntimeConfig
from kagent.core.a2a import KAgentGrpcServerCallContextBuilder, _runtime

from kagent.adk import KAgentApp
from kagent.adk.types import Config


def make_app(
    address: str | None = "127.0.0.1:0", port: int | None = None, max_content_length: int | None = None, lifespan=None
):
    card = ParseDict(
        {
            "name": "test-app",
            "description": "test agent",
            "version": "0.0.1",
            "supportedInterfaces": [{"url": "http://localhost:80", "protocolBinding": "GRPC"}],
            "capabilities": {},
            "defaultInputModes": ["text/plain"],
            "defaultOutputModes": ["text/plain"],
            "skills": [],
        },
        AgentCard(),
    )
    return KAgentApp(
        lifespan=lifespan,
        root_agent_factory=lambda: None,
        agent_card=card,
        kagent_api_url="http://unused",
        app_name="test-app",
        a2a_grpc_address=address,
        config=Config(runtime=RuntimeConfig(port=port, max_content_length=max_content_length)),
    )


@pytest.mark.parametrize("port,expected", [(None, "[::]:80"), (9090, "[::]:9090")])
def test_grpc_address_from_runtime_port(port, expected):
    assert _runtime.grpc_address(RuntimeConfig(port=port)) == expected


@pytest.mark.asyncio
async def test_agent_hook_unwinds_if_readiness_fails(monkeypatch):
    events = []

    @asynccontextmanager
    async def hook(server):
        assert server is app
        events.append("enter")
        try:
            yield
        finally:
            events.append("exit")

    async def fail_readiness(*args, **kwargs):
        raise OSError("readiness port unavailable")

    close = AsyncControllerClient.close

    async def close_controller(client):
        await close(client)
        events.append("controller closed")

    monkeypatch.setattr(AsyncControllerClient, "close", close_controller)
    app = make_app(lifespan=hook).build()
    monkeypatch.setattr(asyncio, "start_server", fail_readiness)
    with pytest.raises(OSError, match="readiness port unavailable"):
        async with app.listen():
            pytest.fail("startup succeeded with no readiness listener")
    assert events == ["enter", "controller closed", "exit"]


@pytest.mark.asyncio
async def test_grpc_health():
    app = make_app().build(local=True)

    async with app.listen() as port:
        async with grpc.aio.insecure_channel(f"127.0.0.1:{port}") as channel:
            response = await health_pb2_grpc.HealthStub(channel).Check(
                health_pb2.HealthCheckRequest(service="lf.a2a.v1.A2AService")
            )

    assert response.status == health_pb2.HealthCheckResponse.SERVING


@pytest.mark.asyncio
async def test_grpc_lists_local_tasks():
    app = make_app().build(local=True)

    async with app.listen() as port:
        async with grpc.aio.insecure_channel(f"127.0.0.1:{port}") as channel:
            response = await a2a_pb2_grpc.A2AServiceStub(channel).ListTasks(a2a_pb2.ListTasksRequest())

    assert response.total_size == 0


@pytest.mark.asyncio
async def test_grpc_enforces_runtime_request_size():
    app = make_app(max_content_length=16).build(local=True)
    async with app.listen() as port:
        async with grpc.aio.insecure_channel(f"127.0.0.1:{port}") as channel:
            stub = a2a_pb2_grpc.A2AServiceStub(channel)
            with pytest.raises(grpc.aio.AioRpcError) as error:
                await stub.ListTasks(a2a_pb2.ListTasksRequest(context_id="x" * 32))
            assert error.value.code() == grpc.StatusCode.RESOURCE_EXHAUSTED


def test_grpc_metadata_preserves_user_identity():
    class FakeContext:
        @staticmethod
        def invocation_metadata():
            return (("x-user-id", "alice"), ("x-kagent-source", "parent"))

    context = KAgentGrpcServerCallContextBuilder().build(FakeContext())

    assert context.user.user_name == "alice"
    assert context.state["headers"]["x-kagent-source"] == "parent"
    assert context.state["kagent_source"] == "parent"


@pytest.mark.asyncio
async def test_grpc_ingress_is_traced_except_health(monkeypatch):
    from opentelemetry.sdk.trace import TracerProvider
    from opentelemetry.sdk.trace.export import SimpleSpanProcessor
    from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
    from opentelemetry.trace import SpanKind

    exporter = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    real_interceptor = _runtime.aio_server_interceptor
    monkeypatch.setattr(
        _runtime, "aio_server_interceptor", lambda **kwargs: real_interceptor(tracer_provider=provider, **kwargs)
    )
    app = make_app().build(local=True)
    trace_id = 0x4BF92F3577B34DA6A3CE929D0E0E4736
    metadata = (("traceparent", f"00-{trace_id:032x}-00f067aa0ba902b7-01"),)

    async with app.listen() as port:
        async with grpc.aio.insecure_channel(f"127.0.0.1:{port}") as channel:
            await health_pb2_grpc.HealthStub(channel).Check(health_pb2.HealthCheckRequest(service=""))
            await a2a_pb2_grpc.A2AServiceStub(channel).ListTasks(a2a_pb2.ListTasksRequest(), metadata=metadata)

    (span,) = exporter.get_finished_spans()
    assert span.kind is SpanKind.SERVER
    assert span.name == "/lf.a2a.v1.A2AService/ListTasks"
    assert span.context.trace_id == trace_id
    assert span.parent.span_id == 0x00F067AA0BA902B7


@pytest.mark.asyncio
async def test_cli_serves_grpc_and_exits_cleanly_on_sigterm(tmp_path, unused_tcp_port):
    agent = tmp_path / "sample"
    agent.mkdir()
    (agent / "__init__.py").write_text("")
    (agent / "agent-card.json").write_text(json.dumps({"name": "sample", "version": "1"}))
    runtime = {
        "name": "sample",
        "namespace": "test",
        "control_plane_url": "http://localhost",
        "gateway_url": "http://localhost",
        "host": "127.0.0.1",
        "port": unused_tcp_port,
    }
    (agent / "config.json").write_text(json.dumps({"runtime": runtime}))
    process = await asyncio.create_subprocess_exec(
        sys.executable,
        "-m",
        "kagent.adk.cli",
        "run",
        "sample",
        "--local",
        "--host",
        "0.0.0.0",
        "--port",
        "8080",
        "--workers",
        "1",
        cwd=tmp_path,
    )
    try:
        async with grpc.aio.insecure_channel(f"127.0.0.1:{unused_tcp_port}") as channel:
            tasks = await a2a_pb2_grpc.A2AServiceStub(channel).ListTasks(
                a2a_pb2.ListTasksRequest(),
                wait_for_ready=True,
                timeout=45,
            )
            assert tasks.total_size == 0
        async with httpx.AsyncClient(trust_env=False) as client:
            assert (await client.get("http://127.0.0.1:8081/readyz")).status_code == 200
        process.terminate()
        assert await asyncio.wait_for(process.wait(), timeout=15) == 0
    finally:
        if process.returncode is None:
            process.kill()
            await process.wait()
