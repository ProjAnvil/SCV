import { Circle } from './index';

// Deliberate two-hop consumer: Circle reaches this file through the
// barrel (app -> index -> circle), which the one-hop re-export rule
// drops and counts (spec 2026-10-20 §12).
export function main(): string {
  const shape = new Circle();
  return shape.draw();
}
