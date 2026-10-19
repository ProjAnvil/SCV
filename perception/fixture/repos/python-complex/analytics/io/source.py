"""Sources: ABC, protocol, context manager and async iteration."""

from __future__ import annotations

import asyncio
from abc import ABC, abstractmethod
from typing import AsyncIterator, Iterable


class Source(ABC):
    @abstractmethod
    def read(self) -> Iterable[str]:
        raise NotImplementedError

    def close(self) -> None:
        """Default no-op hook."""


class FileSource(Source):
    def __init__(self, path: str) -> None:
        self._path = path
        self._handle = None

    def read(self) -> Iterable[str]:
        with open(self._path, encoding="utf-8") as handle:
            return list(handle)

    def close(self) -> None:
        if self._handle is not None:
            self._handle.close()


class AsyncSource(Source):
    def read(self) -> Iterable[str]:
        return list(self._stream())

    async def _stream(self) -> AsyncIterator[str]:
        for chunk in ("a", "b", "c"):
            await asyncio.sleep(0)
            yield chunk
