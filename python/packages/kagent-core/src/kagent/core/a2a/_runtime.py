"""Serve the A2A gRPC endpoint and Substrate readiness probe for a Python runtime."""

from __future__ import annotations

import asyncio
import signal
from collections.abc import AsyncIterator, Callable
from contextlib import AbstractAsyncContextManager, AsyncExitStack, asynccontextmanager

import grpc
from a2a.server.request_handlers import DefaultRequestHandlerV2
from a2a.server.request_handlers.grpc_handler import GrpcHandler
from a2a.types import a2a_pb2, a2a_pb2_grpc
from grpc_health.v1 import health, health_pb2, health_pb2_grpc
from opentelemetry.instrumentation.grpc import aio_server_interceptor, filters

from kagent.core._runtime_config import RuntimeConfig, optional_field
from kagent.core.telemetry import current

from ._config import get_a2a_max_content_length
from ._requests import KAgentGrpcServerCallContextBuilder


def grpc_address(runtime: RuntimeConfig) -> str:
    return f"{runtime.host or '[::]'}:{runtime.port or 80}"


async def _readiness_probe(reader: asyncio.StreamReader, writer: asyncio.StreamWriter):
    try:
        request = await reader.readline()
        status = b"200 OK" if request.startswith(b"GET /readyz ") else b"404 Not Found"
        writer.write(b"HTTP/1.1 " + status + b"\r\nContent-Length: 2\r\nConnection: close\r\n\r\nOK")
        await writer.drain()
    finally:
        writer.close()
        await writer.wait_closed()


class A2AServer:
    """Run a Python agent until interruption or container termination."""

    def __init__(
        self,
        handler: DefaultRequestHandlerV2,
        runtime: RuntimeConfig,
        *resources: AbstractAsyncContextManager | None,
        lifespan: Callable[[A2AServer], AbstractAsyncContextManager] | None = None,
        address: str | None = None,
    ):
        self._handler = handler
        self._address = address or grpc_address(runtime)
        self._resources = resources
        self._lifespan = lifespan
        self._max_content_length = get_a2a_max_content_length(optional_field(runtime, "max_content_length"))

    def run(self) -> None:
        asyncio.run(self._serve())

    async def _serve(self) -> None:
        stop = asyncio.Event()
        loop = asyncio.get_running_loop()
        loop.add_signal_handler(signal.SIGTERM, stop.set)
        try:
            async with self.listen():
                await stop.wait()
        finally:
            loop.remove_signal_handler(signal.SIGTERM)

    @asynccontextmanager
    async def listen(self) -> AsyncIterator[int]:
        """Bind gRPC before publishing readiness and close both on exit."""
        async with AsyncExitStack() as stack:
            if providers := current():
                stack.callback(providers.shutdown)
            if self._lifespan is not None:
                await stack.enter_async_context(self._lifespan(self))
            for resource in self._resources:
                if resource is not None:
                    await stack.enter_async_context(resource)
            stack.push_async_callback(self._handler.aclose)
            server = grpc.aio.server(
                interceptors=[aio_server_interceptor(filter_=filters.negate(filters.health_check()))],
                options=[("grpc.max_receive_message_length", min(self._max_content_length or -1, 2**31 - 1))],
            )
            a2a_pb2_grpc.add_A2AServiceServicer_to_server(
                GrpcHandler(self._handler, context_builder=KAgentGrpcServerCallContextBuilder()), server
            )
            health_service = health.aio.HealthServicer()
            service_name = a2a_pb2.DESCRIPTOR.services_by_name["A2AService"].full_name
            await health_service.set(service_name, health_pb2.HealthCheckResponse.SERVING)
            health_pb2_grpc.add_HealthServicer_to_server(health_service, server)
            port = server.add_insecure_port(self._address)
            if port == 0:
                raise RuntimeError(f"failed to bind A2A gRPC server to {self._address}")
            await server.start()
            try:
                # Substrate may probe over IPv4 and snapshot as soon as readiness succeeds.
                async with await asyncio.start_server(_readiness_probe, host=None, port=8081):
                    yield port
            finally:
                await health_service.enter_graceful_shutdown()
                await server.stop(grace=5)
