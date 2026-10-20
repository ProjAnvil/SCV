import { Shape, Circle } from "./shapes";
import { Base } from "./base";
import { format } from "./format";
import type { Report } from "./report";

export class Service extends Base implements Shape {
  area(): number {
    return 1;
  }
  scale(factor: number): void {
    this.area();
    this.describe();
  }
  process(shape: Shape, factor: number): string {
    shape.scale(factor);
    return format(this.area());
  }
  render(circle: Circle): void {
    circle.area();
  }
  emit(report: Report): void {
    report.render();
  }
}
