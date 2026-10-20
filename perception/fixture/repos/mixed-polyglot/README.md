# mixed-polyglot (perception fixture)

Java + Python + TypeScript in one tree, used as the v3 multi-language
ground truth: a single perceive.py run must produce ONE structure.json /
structure.vg whose nodes carry `lang`, with `meta.languages` naming all
three and `meta.structure.by_lang` holding the per-language split.

- java: minimal `src/main/java` pair — `Greetable` (interface) and
  `Greeter implements Greetable` with an explicit single-type import
  (java IMPORTS File->File)
- python: a two-file package — `py/__init__.py` re-exports
  `from .worker import Worker`; `Worker.start` calls `self.prepare()`
- ts: a two-file pair — `Notifiable` interface and
  `Panel implements Notifiable` with a relative import

## Frozen ground truth (hand-derived, spec §11.3)

- `meta.languages == ["java", "python", "typescript"]`
- files 6 / types 5 / methods 6 / call_edges 1
- `by_lang`: java {2,2,2,0}, python {2,1,2,1}, typescript {2,2,2,0}
  (files, types, methods, call_edges)
- IMPLEMENTS 2, both `kind: declared`:
  `type:com.acme.bridge.Greeter -> type:com.acme.bridge.Greetable` (java),
  `type:ts/panel::Panel -> type:ts/contracts::Notifiable` (ts)
- IMPORTS 3 — one per language, coexisting in the same graph:
  Greeter.java -> Greetable.java, py/__init__.py -> py/worker.py,
  ts/panel.ts -> ts/contracts.ts
- CALLS 1: `fn:py.worker.Worker#start(0) -> fn:py.worker.Worker#prepare(0)`
  (direct)
- CONTAINS 5; `dropped_edges` 0; `unresolved_refs` 0
