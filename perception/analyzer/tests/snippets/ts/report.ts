export interface Report {
  render(): string;
}

export default class Session {
  start(): void {
    console.log("session");
  }
}
