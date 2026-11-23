import { Component, computed, inject, input, output } from '@angular/core';
import { RequestApiService } from '../core/services/request.service';

/**
 * The Send button: "primary" is the request list's main action, "success"
 * Zen mode's. While its request is in flight the inspector shows the send
 * (RequestInFlight); the button can still be clicked to send again.
 */
@Component({
  selector: 'app-send-button',
  template: `
    <button
      class="send-plane"
      type="button"
      [class.send-plane--primary]="variant() === 'primary'"
      [class.send-plane--success]="variant() === 'success'"
      [attr.aria-busy]="inFlight()"
      [attr.aria-label]="label()"
      [title]="label()"
      (click)="send.emit($event)"
    >
      <span class="send-plane-text">Send</span>
    </button>
  `,
  styleUrl: './send-button.scss',
})
export class SendButton {
  private readonly requestApi = inject(RequestApiService);

  readonly requestId = input<number | null>(null);
  readonly variant = input<'primary' | 'success'>('primary');
  readonly label = input('Send (Ctrl+S)');
  readonly send = output<MouseEvent>();

  protected readonly inFlight = computed(() => {
    const id = this.requestId();
    return id !== null && this.requestApi.sending()[id] !== undefined;
  });
}
