import { Component, DestroyRef, computed, effect, inject, input, output, signal, untracked } from '@angular/core';
import { RequestApiService } from '../core/services/request.service';

/** Keep in sync with the keyframes in send-button.scss. */
const LANDING_MS = 450;
/** Fast requests answer before this and only get the landing, no sending animation. */
const SENDING_DELAY_MS = 150;

/** How a send ended: a response arrived, or there was none (status 0). */
type Landing = 'done' | 'failed';

/**
 * The Send button. By default it reads "Send"; themes can turn it into a
 * character: Paper Plane shows a paper plane, Space Groove an eye looking
 * for something, Collage an archer shooting at hanging fish (each theme's CSS
 * shows its icon and hides the text). The button sets the state classes
 * is-sending / is-done / is-failed, plus is-client-error / is-server-error
 * for 4xx / 5xx responses; each icon animates them its own way.
 * While its request is in flight (from this button, Ctrl+S or anywhere else)
 * the plane glides with wind lines behind it; when the response arrives it
 * flies off and back, or wobbles when the request failed (no response:
 * status 0). Sending again while in flight is fine. "primary" is the request
 * list's main action, "success" Zen mode's.
 *
 * The plane always animates, whatever the system's reduce-motion setting:
 * it is the app's signature, and the motion stays inside the button.
 */
@Component({
  selector: 'app-send-button',
  template: `
    <button
      class="send-plane"
      type="button"
      [class.send-plane--primary]="variant() === 'primary'"
      [class.send-plane--success]="variant() === 'success'"
      [class.is-sending]="sendingVisible()"
      [class.is-done]="landing() === 'done'"
      [class.is-failed]="landing() === 'failed'"
      [class.is-client-error]="landing() === 'done' && landingStatus() >= 400 && landingStatus() < 500"
      [class.is-server-error]="landing() === 'done' && landingStatus() >= 500"
      [attr.aria-busy]="inFlight()"
      [attr.aria-label]="inFlight() ? 'Sending…' : label()"
      [title]="inFlight() ? 'Sending…' : label()"
      (click)="send.emit($event)"
    >
      <span class="send-plane-text">Send</span>
      <!-- Collage: an archer shoots at hanging fish. Hidden by every other theme. -->
      <span class="send-scene" aria-hidden="true">
        <span class="send-archer"></span>
        <span class="send-bow"></span>
        <span class="send-arrow"></span>
        <span class="send-fish send-fish--a"></span>
        <span class="send-fish send-fish--b"></span>
      </span>
      <span class="send-plane-wind" aria-hidden="true"><i></i><i></i><i></i></span>
      <svg class="send-eye" viewBox="0 0 32 20" aria-hidden="true">
        <g class="send-eye-lid">
          <path class="send-eye-white" d="M2 10 Q16 -1 30 10 Q16 21 2 10 Z" />
          <g class="send-eye-iris">
            <circle class="send-eye-iris-ring" cx="16" cy="10" r="5.2" />
            <circle class="send-eye-pupil" cx="16" cy="10" r="2.4" />
            <circle class="send-eye-glint" cx="17.6" cy="8.4" r="1" />
          </g>
          <path class="send-eye-outline" d="M2 10 Q16 -1 30 10 Q16 21 2 10 Z" />
        </g>
      </svg>
      <svg class="send-plane-icon" viewBox="0 0 24 24" aria-hidden="true">
        <path d="M22 2 11 13" />
        <path d="M22 2 15 22l-4-9-9-4 20-7z" />
      </svg>
    </button>
  `,
  styleUrl: './send-button.scss',
})
export class SendButton {
  private readonly requestApi = inject(RequestApiService);

  /** The request this button sends; its in-flight state drives the plane. */
  readonly requestId = input<number | null>(null);
  readonly variant = input<'primary' | 'success'>('primary');
  readonly label = input('Send (Ctrl+S)');
  readonly send = output<MouseEvent>();

  protected readonly inFlight = computed(() => {
    const id = this.requestId();
    return id !== null && this.requestApi.sending()[id] !== undefined;
  });
  /** In flight long enough to show it (see SENDING_DELAY_MS). */
  protected readonly sendingVisible = signal(false);
  protected readonly landing = signal<Landing | null>(null);
  /** HTTP status of the response that just landed (0 when there was none). */
  protected readonly landingStatus = signal(0);

  private sendingTimer: ReturnType<typeof setTimeout> | null = null;
  private landingTimer: ReturnType<typeof setTimeout> | null = null;

  constructor() {
    // Show sending only once a request has been in flight a moment, so
    // quick responses don't flicker.
    effect(() => {
      const flying = this.inFlight();
      untracked(() => {
        this.clearSendingTimer();
        if (!flying) {
          this.sendingVisible.set(false);
          return;
        }
        this.sendingTimer = setTimeout(() => this.sendingVisible.set(this.inFlight()), SENDING_DELAY_MS);
      });
    });

    const landed = this.requestApi.finished$.subscribe((response) => {
      if (response.request_id !== this.requestId()) return;
      this.landingStatus.set(response.status_code);
      this.land(response.status_code === 0 ? 'failed' : 'done');
    });

    inject(DestroyRef).onDestroy(() => {
      landed.unsubscribe();
      this.clearSendingTimer();
      if (this.landingTimer) clearTimeout(this.landingTimer);
    });
  }

  private land(kind: Landing): void {
    if (this.landingTimer) clearTimeout(this.landingTimer);
    // Restart the animation even if one is already playing.
    this.landing.set(null);
    requestAnimationFrame(() => {
      this.landing.set(kind);
      this.landingTimer = setTimeout(() => this.landing.set(null), LANDING_MS);
    });
  }

  private clearSendingTimer(): void {
    if (this.sendingTimer) clearTimeout(this.sendingTimer);
    this.sendingTimer = null;
  }
}
