// Barrel: one-hop re-exports only — the extractor resolves these to the
// declaring modules. src/app.ts exercises the two-hop failure mode.
export { Shape, Renderable } from './types';
export { BaseShape } from './models/base';
export { Circle } from './models/circle';
export { Polygon } from './models/polygon';
