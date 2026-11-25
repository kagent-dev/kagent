"""Lifespan manager for composing multiple FastAPI lifespans."""

from contextlib import asynccontextmanager
from typing import AsyncIterator, Callable, List

from fastapi import FastAPI


class LifespanManager:
    """
    A simple lifespan manager that composes multiple FastAPI lifespans.
    Inspired by https://github.com/uriyyo/fastapi-lifespan-manager
    """

    def __init__(self) -> None:
        self._lifespans: List[Callable[[FastAPI], AsyncIterator[None]]] = []

    def add(self, lifespan: Callable[[FastAPI], AsyncIterator[None]]) -> None:
        """Add a context manager to the manager."""
        if lifespan is not None:
            self._lifespans.append(lifespan)

    @asynccontextmanager
    async def __call__(self, app: FastAPI) -> AsyncIterator[None]:
        """Compose all lifespans into a single context manager."""

        async def nested(index: int) -> AsyncIterator[None]:
            if index >= len(self._lifespans):
                yield
            else:
                async with self._lifespans[index](app):
                    async for _ in nested(index + 1):
                        yield

        async for _ in nested(0):
            yield
