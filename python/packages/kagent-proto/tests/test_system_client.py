import grpc
import pytest
from kagent.api.v1alpha1 import system_pb2, system_pb2_grpc


class SystemService(system_pb2_grpc.SystemServiceServicer):
    def __init__(self) -> None:
        self.metadata: dict[str, str] = {}
        self.time_remaining: float | None = None

    async def GetVersion(self, request, context):
        self.metadata = dict(context.invocation_metadata())
        self.time_remaining = context.time_remaining()
        return system_pb2.GetVersionResponse(
            kagent_version="v1.2.3",
            git_commit="abc123",
            build_date="2026-07-28",
        )


@pytest.mark.asyncio
async def test_generated_async_client_forwards_metadata_and_deadline() -> None:
    service = SystemService()
    server = grpc.aio.server()
    system_pb2_grpc.add_SystemServiceServicer_to_server(service, server)
    port = server.add_insecure_port("127.0.0.1:0")
    await server.start()

    try:
        async with grpc.aio.insecure_channel(f"127.0.0.1:{port}") as channel:
            response = await system_pb2_grpc.SystemServiceStub(channel).GetVersion(
                system_pb2.GetVersionRequest(),
                timeout=5,
                metadata=(
                    ("authorization", "Bearer token"),
                    ("x-share-token", "share-token"),
                ),
            )
    finally:
        await server.stop(grace=0)

    assert response == system_pb2.GetVersionResponse(
        kagent_version="v1.2.3",
        git_commit="abc123",
        build_date="2026-07-28",
    )
    assert service.metadata["authorization"] == "Bearer token"
    assert service.metadata["x-share-token"] == "share-token"
    assert service.time_remaining is not None
    assert 0 < service.time_remaining <= 5
