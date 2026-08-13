import grpc
import pytest
from kagent.api.v1alpha1 import memory_pb2, memory_pb2_grpc


class MemoryService(memory_pb2_grpc.MemoryServiceServicer):
    def __init__(self) -> None:
        self.metadata: dict[str, str] = {}
        self.time_remaining: float | None = None

    async def Search(self, request, context):
        self.metadata = dict(context.invocation_metadata())
        self.time_remaining = context.time_remaining()
        return memory_pb2.MemoryServiceSearchResponse(
            memories=[memory_pb2.MemorySearchResult(id="memory-1", content="remember me", score=0.9)]
        )


@pytest.mark.asyncio
async def test_generated_async_client_forwards_metadata_and_deadline() -> None:
    service = MemoryService()
    server = grpc.aio.server()
    memory_pb2_grpc.add_MemoryServiceServicer_to_server(service, server)
    port = server.add_insecure_port("127.0.0.1:0")
    await server.start()

    try:
        async with grpc.aio.insecure_channel(f"127.0.0.1:{port}") as channel:
            response = await memory_pb2_grpc.MemoryServiceStub(channel).Search(
                memory_pb2.MemoryServiceSearchRequest(agent_name="agent", user_id="user", vector=[1.0]),
                timeout=5,
                metadata=(
                    ("authorization", "Bearer token"),
                    ("x-share-token", "share-token"),
                ),
            )
    finally:
        await server.stop(grace=0)

    assert response == memory_pb2.MemoryServiceSearchResponse(
        memories=[memory_pb2.MemorySearchResult(id="memory-1", content="remember me", score=0.9)]
    )
    assert service.metadata["authorization"] == "Bearer token"
    assert service.metadata["x-share-token"] == "share-token"
    assert service.time_remaining is not None
    assert 0 < service.time_remaining <= 5.5
