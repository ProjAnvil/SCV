from app.base import Base
from app.util import helper


class Greeter(Base):
    def greet(self, name):
        text = self._fmt(name)
        return text

    def _rec_a(self, n):
        if n <= 0:
            return 0
        return self._rec_b(n - 1)

    def _rec_b(self, n):
        return self._rec_a(n - 1)


def make(prefix):
    value = helper(prefix, 1)
    return value
