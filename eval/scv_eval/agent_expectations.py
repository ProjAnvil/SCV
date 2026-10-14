"""Custom smelt expectations for the project-analyzer agent eval."""

from __future__ import annotations

from dataclasses import dataclass

from smelt.results import ExpectationResult
from smelt.trace import Trace


@dataclass(frozen=True)
class WriteContains:
    """Assert the agent wrote a file whose path contains ``path_part`` and whose
    content contains ``content_part``.

    Scoring mirrors ToolCallExpectation: full match 1.0; the file was written
    but the content fragment is missing 0.5 (right direction, wrong details);
    file never written 0.0.
    """

    path_part: str
    content_part: str
    threshold: float = 1.0

    @property
    def name(self) -> str:
        return f"write_contains({self.path_part!r}, {self.content_part!r})"

    def evaluate(self, trace: Trace) -> ExpectationResult:
        calls = trace.calls_named("Write")
        path_hits = [c for c in calls
                     if self.path_part in str(c.arguments.get("file_path", ""))]
        if not path_hits:
            return ExpectationResult(
                name=self.name, score=0.0, threshold=self.threshold,
                message=f"Write to *{self.path_part}* never happened; "
                        f"written files: {[str(c.arguments.get('file_path')) for c in calls] or '(none)'}")
        for call in path_hits:
            if self.content_part in str(call.arguments.get("content", "")):
                return ExpectationResult(
                    name=self.name, score=1.0, threshold=self.threshold,
                    message="content fragment found",
                    details={"matched": dict(call.arguments)})
        return ExpectationResult(
            name=self.name, score=0.5, threshold=self.threshold,
            message=f"*{self.path_part}* written but content lacks {self.content_part!r}",
            details={"actual": [str(c.arguments.get("content", ""))[:400] for c in path_hits]})
