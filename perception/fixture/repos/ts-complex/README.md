# ts-complex (perception fixture)

A TypeScript + JavaScript mixed tree used as a positive ground truth for
SCV perception v3. It is not meant to typecheck against a real
`node_modules`; it exists so the analyzer sees every jsts edge kind in
one small repo.

Covers on purpose:

- TS `interface` + `implements` (multi-clause) + `extends` + `type alias`
- `this.x()` calls, annotated class-typed receivers (direct), and
  interface-typed receivers (`virtual-dispatch`)
- a CommonJS `.js` file: relative `require`, class `this` calls, and one
  unknown free call (`mystery()`) — the honest `unresolved_refs` floor
- a barrel `index.ts` of one-hop re-exports
- `src/app.ts` imports a symbol **through** the barrel (two hops:
  app -> index -> circle) — dropped and counted, per spec §12
- `src/services/render.ts` has one bare npm import (`lodash-es`) —
  dropped and counted, never an edge

## Frozen ground truth (hand-derived, spec §11.2)

Hand-counted from the source tree against the extractor rules
(jsts_extractor: TS param keys use annotation simple names, JS keys use
arity; re-export binding is one-hop only):

- nodes: 8 File, 8 Type (Shape, Renderable, ShapeList interface/alias in
  `src/types`; BaseShape, Circle, Polygon, Renderer classes; Scanner in
  `src/legacy/util.js`), 21 Method
- CONTAINS 8; EXTENDS 1 (Polygon -> BaseShape); IMPLEMENTS 4
  (BaseShape->Shape, Circle->Shape, Circle->Renderable, Polygon->Shape,
  all `kind: declared`)
- CALLS 6: Circle#draw->Circle#area (direct), Renderer#banner->Renderer#head
  (direct), Renderer#banner->Shape#name (virtual-dispatch),
  Renderer#outline->Circle#draw (direct), Renderer#scale->Shape#area
  (virtual-dispatch), Scanner#add->Scanner#describe (direct)
- REFERENCES 4: Renderer#banner->Shape, Renderer#outline->Circle,
  Renderer#scale->Shape (param annotations), scanCircle->Circle (`new`)
- IMPORTS 12: base/circle/polygon/render -> types (+polygon -> base,
  render -> circle), util.js -> circle.ts, index -> types/base/circle/
  polygon (one-hop re-exports), app -> index
- `dropped_edges` 2: the `lodash-es` bare import + app's two-hop Circle
- `unresolved_refs` 3: `new Circle()` in app (binding dropped two hops
  earlier), `shape.draw()` in app, `mystery()` in util.js
- `meta.languages == ["javascript", "typescript"]`
