"""Serve the A2A gRPC endpoint and Substrate readiness probe for a Python runtime."""

import asyncio
from collections.abc import Callable
from contextlib import AbstractAsyncContextManager, asynccontextmanager

import grpc
from a2a.server.request_handlers import DefaultRequestHandlerV2
from a2a.server.request_handlers.grpc_handler import GrpcHandler
from a2a.types import a2a_pb2, a2a_pb2_grpc
from fastapi import FastAPI
from grpc_health.v1 import health, health_pb2, health_pb2_grpc
from opentelemetry.instrumentation.grpc import aio_server_interceptor, filters

from kagent.core._runtime_config import RuntimeConfig

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


def runtime_lifespan(
    request_handler: DefaultRequestHandlerV2,
    address: str,
    inner: Callable[[FastAPI], AbstractAsyncContextManager],
):
    @asynccontextmanager
    async def lifespan(app: FastAPI):
        async with inner(app):
            server = grpc.aio.server(
                interceptors=[aio_server_interceptor(filter_=filters.negate(filters.health_check()))]
            )
            a2a_pb2_grpc.add_A2AServiceServicer_to_server(
                GrpcHandler(request_handler, context_builder=KAgentGrpcServerCallContextBuilder()), server
            )
            health_service = health.aio.HealthServicer()
            service_name = a2a_pb2.DESCRIPTOR.services_by_name["A2AService"].full_name
            await health_service.set(service_name, health_pb2.HealthCheckResponse.SERVING)
            health_pb2_grpc.add_HealthServicer_to_server(health_service, server)
            port = server.add_insecure_port(address)
            if port == 0:
                raise RuntimeError(f"failed to bind A2A gRPC server to {address}")
            app.state.a2a_grpc_port = port
            await server.start()
            try:
                # Substrate may probe over IPv4 and snapshot as soon as readiness succeeds.
                async with await asyncio.start_server(_readiness_probe, host=None, port=8081):
                    yield
            finally:
                await health_service.enter_graceful_shutdown()
                await server.stop(grace=5)

    return lifespan
