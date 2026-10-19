"""Pipeline: decorators, mixins, metaclass and mutual recursion."""

from __future__ import annotations

from functools import wraps
from typing import Any, Callable, Iterable

from ..io.source import Source
from ..model.event import Event, Ring, Severity


def timed(func: Callable[..., Any]) -> Callable[..., Any]:
    @wraps(func)
    def wrapper(*args: Any, **kwargs: Any) -> Any:
        return func(*args, **kwargs)

    return wrapper


class RegistryMeta(type):
    """Metaclass that records every subclass — exercises metaclasses."""

    registry: dict[str, type] = {}

    def __new__(mcls, name, bases, namespace):
        cls = super().__new__(mcls, name, bases, namespace)
        if bases:
            mcls.registry[name] = cls
        return cls


class Serializable:
    """Mixin providing a default dump."""

    def dump(self) -> dict[str, Any]:
        return {"type": type(self).__name__}


class Pipeline(Serializable, metaclass=RegistryMeta):
    def __init__(self, source: Source, capacity: int = 128) -> None:
        self._source = source
        self._ring: Ring[Event] = Ring(capacity)

    @timed
    def run(self) -> list[Event]:
        events = [self._parse(line) for line in self._source.read()]
        for event in events:
            self._ring.push(event)
        return [self._dedupe(event) for event in events]

    def _parse(self, line: str) -> Event:
        severity = Severity.ERROR if "error" in line else Severity.INFO
        return Event(line.strip(), severity)

    def _dedupe(self, event: Event) -> Event:
        # mutual recursion with _flatten on purpose
        return self._flatten(event, 0)

    def _flatten(self, event: Event, depth: int) -> Event:
        if depth > 3 or event.severity is not Severity.INFO:
            return event
        return self._dedupe(event.escalate())
