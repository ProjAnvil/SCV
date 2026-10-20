"""A worker module for the polyglot fixture."""


class Worker:
    """Starts itself in two steps."""

    def start(self):
        return self.prepare()

    def prepare(self):
        return "ready"
