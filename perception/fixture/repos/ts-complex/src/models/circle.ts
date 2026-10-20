import { Renderable, Shape } from '../types';

// Circle implements two interfaces at once (multi-clause implements).
export class Circle implements Shape, Renderable {
  radius: number = 1;

  name(): string {
    return "circle";
  }

  area(): number {
    return 3 * this.radius * this.radius;
  }

  draw(): string {
    return `circle(${this.area()})`;
  }
}
