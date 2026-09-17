from __future__ import annotations

import asyncio
from contextlib import asynccontextmanager
from pathlib import Path
from typing import Any, Protocol
from urllib.parse import urlsplit

import grpc
from kagent.api.v1alpha1 import memory_pb2_grpc

DEFAULT_TIMEOUT_SECONDS = 30.0
DEFAULT_MAX_MESSAGE_BYTES = 16 << 20
DEFAULT_TOKEN_PATH = "/var/run/secrets/tokens/kagent-token"


class AsyncTokenProvider(Protocol):
    async def get_token(self) -> str | None: ...


class AsyncFileTokenProvider:
    """Read the current projected service-account token for each RPC."""

    def __init__(self, path: str = DEFAULT_TOKEN_PATH) -> None:
        self.path = Path(path)

    async def get_token(self) -> str | None:
        try:
            token = await asyncio.to_thread(self.path.read_text, encoding="utf-8")
        except OSError:
            return None
        return token.strip() or None


class AsyncControllerClient:
    """Shared authenticated ``grpc.aio`` channel for controller services."""

    def __init__(
        self,
        api_url: str | None = None,
        *,
        agent_name: str = "",
        token_provider: AsyncTokenProvider | None = None,
        timeout: float = DEFAULT_TIMEOUT_SECONDS,
        max_message_bytes: int = DEFAULT_MAX_MESSAGE_BYTES,
        channel: grpc.aio.Channel | None = None,
        credentials: grpc.ChannelCredentials | None = None,
    ) -> None:
        if channel is None and not api_url:
            raise ValueError("controller API URL is required")

        self.target, self.secure = _target_from_url(api_url) if api_url else (None, False)
        self.timeout = timeout
        self.max_message_bytes = max_message_bytes
        self.agent_name = agent_name
        self.token_provider = token_provider
        self.credentials = credentials
        self._owns_channel = channel is None
        self._channel = channel
        self._closed = False
        self._memory_service: memory_pb2_grpc.MemoryServiceStub | None = None

    @property
    def channel(self) -> grpc.aio.Channel:
        if self._channel is None:
            if self._closed:
                raise RuntimeError("controller gRPC client is closed")
            options = (
                ("grpc.max_receive_message_length", self.max_message_bytes),
                ("grpc.max_send_message_length", self.max_message_bytes),
            )
            if self.credentials is not None:
                self._channel = grpc.aio.secure_channel(self.target, self.credentials, options=options)
            elif self.secure:
                self._channel = grpc.aio.secure_channel(self.target, grpc.ssl_channel_credentials(), options=options)
            else:
                self._channel = grpc.aio.insecure_channel(self.target, options=options)
        return self._channel

    @property
    def memory_service(self) -> memory_pb2_grpc.MemoryServiceStub:
        if self._memory_service is None:
            self._memory_service = memory_pb2_grpc.MemoryServiceStub(self.channel)
        return self._memory_service

    async def call_options(self, user_id: str | None = None) -> dict[str, Any]:
        metadata: list[tuple[str, str]] = []
        if self.token_provider is not None:
            token = await self.token_provider.get_token()
            if token:
                metadata.append(("authorization", f"Bearer {token}"))
        if self.agent_name:
            metadata.append(("x-agent-name", self.agent_name))
        if not user_id:
            from .a2a._context import get_request_user_id

            user_id = get_request_user_id()
        if user_id:
            metadata.append(("x-user-id", user_id))
        return {"metadata": metadata, "timeout": self.timeout}

    async def close(self) -> None:
        if self._closed:
            return
        self._closed = True
        if self._owns_channel and self._channel is not None:
            await self._channel.close()

    def lifespan(self):
        @asynccontextmanager
        async def _lifespan(_: Any):
            try:
                yield
            finally:
                await self.close()

        return _lifespan

    async def __aenter__(self) -> AsyncControllerClient:
        return self

    async def __aexit__(self, *_: object) -> None:
        await self.close()


def _target_from_url(raw_url: str) -> tuple[str, bool]:
    parsed = urlsplit(raw_url)
    if not parsed.netloc or parsed.path not in ("", "/") or parsed.query or parsed.fragment:
        raise ValueError(f"controller API URL {raw_url!r} must contain only a scheme and authority")
    if parsed.scheme not in ("http", "https"):
        raise ValueError(f"controller API URL {raw_url!r} must use http or https")
    return parsed.netloc, parsed.scheme == "https"
