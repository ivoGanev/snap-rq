import { Component, input, output, signal } from '@angular/core';

/** How long the plane's fly-off lasts; keep in sync with the keyframes. */
const FLIGHT_MS = 450;

/**
 * The Send button: a paper plane that flies off when clicked. "primary" is
 * the request list's main action, "success" the green one in Zen mode.
 */
@Component({
  selector: 'app-send-button',
  template: `
    <button
      class="send-plane"
      type="button"
      [class.send-plane--primary]="variant() === 'primary'"
      [class.send-plane--success]="variant() === 'success'"
      [class.is-flying]="flying()"
      [attr.aria-label]="label()"
      [title]="label()"
      (click)="onClick($event)"
    >
      <svg class="send-plane-icon" viewBox="0 0 24 24" aria-hidden="true">
        <path d="M22 2 11 13" />
        <path d="M22 2 15 22l-4-9-9-4 20-7z" />
      </svg>
    </button>
  `,
  styleUrl: './send-button.scss',
})
export class SendButton {
  readonly variant = input<'primary' | 'success'>('primary');
  readonly label = input('Send (Ctrl+S)');
  readonly send = output<MouseEvent>();

  protected readonly flying = signal(false);

  protected onClick(event: MouseEvent): void {
    this.flying.set(true);
    setTimeout(() => this.flying.set(false), FLIGHT_MS);
    this.send.emit(event);
  }
}
