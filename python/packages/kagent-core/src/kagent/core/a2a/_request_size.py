from collections.abc import Collection

from starlette.exceptions import HTTPException
from starlette.types import ASGIApp, Message, Receive, Scope, Send


class A2ARequestSizeLimitMiddleware:
    """Limit A2A request bodies without buffering them in memory."""

    def __init__(
        self,
        app: ASGIApp,
        max_content_length: int | None,
        rpc_paths: Collection[str] = ("/",),
    ) -> None:
        self.app = app
        self.max_content_length = max_content_length
        self.rpc_paths = frozenset(rpc_paths)

    async def __call__(self, scope: Scope, receive: Receive, send: Send) -> None:
        if (
            self.max_content_length is None
            or scope["type"] != "http"
            or scope["method"] != "POST"
            or scope["path"] not in self.rpc_paths
        ):
            await self.app(scope, receive, send)
            return

        content_length = _get_content_length(scope)
        received = 0

        async def limited_receive() -> Message:
            nonlocal received

            # Reject a declared oversized body before reading it. The A2A
            # dispatcher converts this exception into its JSON-RPC error format.
            if content_length is not None and content_length > self.max_content_length:
                raise HTTPException(status_code=413, detail="Payload too large")

            message = await receive()
            if message["type"] == "http.request":
                received += len(message.get("body", b""))
                if received > self.max_content_length:
                    raise HTTPException(status_code=413, detail="Payload too large")
            return message

        await self.app(scope, limited_receive, send)


def _get_content_length(scope: Scope) -> int | None:
    for name, value in scope["headers"]:
        if name.lower() != b"content-length":
            continue
        try:
            content_length = int(value)
        except ValueError:
            return None
        return content_length if content_length >= 0 else None
    return None
