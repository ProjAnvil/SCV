import { Notifiable } from './contracts';

export class Panel implements Notifiable {
  notify(): string {
    return "panel";
  }
}
