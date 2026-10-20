// Shared geometry contracts for the fixture.
export interface Shape {
  area(): number;
  name(): string;
}

export interface Renderable {
  draw(): string;
}

export type ShapeList = Shape[];
