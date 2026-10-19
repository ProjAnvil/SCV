"""Domain models: dataclasses, enums and a generic container."""

from __future__ import annotations

from dataclasses import dataclass, field
from enum import Enum
from typing import Generic, Iterable, Iterator, TypeVar

T = TypeVar("T")


class Severity(str, Enum):
    INFO = "info"
    WARN = "warn"
    ERROR = "error"

    def at_least(self, other: "Severity") -> bool:
        order = {Severity.INFO: 0, Severity.WARN: 1, Severity.ERROR: 2}
        return order[self] >= order[other]


@dataclass(frozen=True, slots=True)
class Event:
    name: str
    severity: Severity = Severity.INFO
    payload: dict[str, object] = field(default_factory=dict)

    def escalate(self) -> "Event":
        if self.severity is Severity.INFO:
            return Event(self.name, Severity.WARN, dict(self.payload))
        return Event(self.name, Severity.ERROR, dict(self.payload))


class Ring(Generic[T]):
    """Fixed-size buffer: exercises generics and dunder methods."""

    def __init__(self, capacity: int) -> None:
        self._items: list[T] = []
        self._capacity = capacity

    def push(self, item: T) -> None:
        if len(self._items) == self._capacity:
            self._items.pop(0)
        self._items.append(item)

    def __len__(self) -> int:
        return len(self._items)

    def __iter__(self) -> Iterator[T]:
        return iter(self._items)
