import { Component, DestroyRef, computed, effect, inject, input, output, signal, untracked } from '@angular/core';
import { RequestApiService } from '../core/services/request.service';

/** Keep in sync with the keyframes in send-button.scss. */
const LANDING_MS = 450;
/** Fast requests answer before this and only get the landing, no glide. */
const GLIDE_DELAY_MS = 150;

type Landing = 'fly' | 'wobble';

/**
 * The Send button. By default it reads "Send"; the Paper Plane theme turns
 * it into a paper plane (paper-plane.css shows the plane, hides the text).
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
      [class.is-gliding]="gliding()"
      [class.is-flying]="landing() === 'fly'"
      [class.is-wobbling]="landing() === 'wobble'"
      [attr.aria-busy]="inFlight()"
      [attr.aria-label]="inFlight() ? 'Sending…' : label()"
      [title]="inFlight() ? 'Sending…' : label()"
      (click)="send.emit($event)"
    >
      <span class="send-plane-text">Send</span>
      <span class="send-plane-wind" aria-hidden="true"><i></i><i></i><i></i></span>
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
  protected readonly gliding = signal(false);
  protected readonly landing = signal<Landing | null>(null);

  private glideTimer: ReturnType<typeof setTimeout> | null = null;
  private landingTimer: ReturnType<typeof setTimeout> | null = null;

  constructor() {
    // Glide only once a request has been in flight a moment, so quick
    // responses don't flicker.
    effect(() => {
      const flying = this.inFlight();
      untracked(() => {
        this.clearGlideTimer();
        if (!flying) {
          this.gliding.set(false);
          return;
        }
        this.glideTimer = setTimeout(() => this.gliding.set(this.inFlight()), GLIDE_DELAY_MS);
      });
    });

    const landed = this.requestApi.finished$.subscribe((response) => {
      if (response.request_id !== this.requestId()) return;
      this.land(response.status_code === 0 ? 'wobble' : 'fly');
    });

    inject(DestroyRef).onDestroy(() => {
      landed.unsubscribe();
      this.clearGlideTimer();
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

  private clearGlideTimer(): void {
    if (this.glideTimer) clearTimeout(this.glideTimer);
    this.glideTimer = null;
  }
}
