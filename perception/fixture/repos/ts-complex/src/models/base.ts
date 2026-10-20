import { Shape } from '../types';

// Base geometry with a plain label; model fixtures extend this.
export class BaseShape implements Shape {
  label: string = "";

  name(): string {
    return this.label;
  }

  area(): number {
    return 0;
  }
}
