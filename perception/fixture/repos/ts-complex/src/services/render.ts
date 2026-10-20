import { Circle } from '../models/circle';
import { Shape } from '../types';
import { debounce } from 'lodash-es'; // bare npm specifier — honestly dropped

// Renderer exercises this-calls, annotated class receivers and
// interface-typed receivers (virtual dispatch), plus the bare-import
// honesty rule.
export class Renderer {
  prefix: string = "> ";

  head(): string {
    return this.prefix;
  }

  banner(shape: Shape): string {
    return this.head() + shape.name();
  }

  outline(circle: Circle): string {
    return circle.draw();
  }

  scale(shape: Shape, factor: number): number {
    return shape.area() * factor;
  }
}
