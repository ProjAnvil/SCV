import { Shape } from '../types';
import { BaseShape } from './base';

// Polygon exercises `extends` (single inheritance) plus implements.
export class Polygon extends BaseShape implements Shape {
  sides: number = 3;

  area(): number {
    return this.sides * 10;
  }

  perimeter(): number {
    return this.sides * 30;
  }
}
