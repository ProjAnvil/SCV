export interface Shape {
  area(): number;
  scale(factor: number): void;
}

export class Circle implements Shape {
  constructor(private radius: number) {}
  area(): number {
    return this.radius * this.radius * 3;
  }
  scale(factor: number): void {
    this.area();
  }
}
