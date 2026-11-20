import { Component, DestroyRef, computed, effect, inject, input, output, signal, untracked } from '@angular/core';
import { RequestApiService } from '../core/services/request.service';
import { ThemeService } from '../core/services/theme.service';

/** Keep in sync with the keyframes in send-button.scss. */
const LANDING_MS = 450;
/** Fast requests answer before this and only get the landing, no sending animation. */
const SENDING_DELAY_MS = 150;
/** How long a theme's "it failed" wording stays on the button. */
const FAILED_NOTICE_MS = 2500;

/** How a send ended: a response arrived, or there was none (status 0). */
type Landing = 'done' | 'failed';

/**
 * The Send button. By default it reads "Send"; themes turn it into a
 * character through their CSS: Paper Plane shows a paper plane, Space Groove
 * an eye looking for something, AI Slop a shimmering "✨ Generate" with
 * thinking dots and a sparkle burst. Themes can also reword its text
 * ("copy" in themes.json: send.label, send.sending, send.failed).
 *
 * It follows its request's in-flight state (from this button, Ctrl+S or
 * anywhere else) and sets the state classes is-sending / is-done / is-failed
 * (failed = no response, status 0); each character animates them its own
 * way. Sending again while in flight is fine. "primary" is the request
 * list's main action, "success" Zen mode's.
 *
 * The characters always animate, whatever the system's reduce-motion
 * setting: they are the themes' signatures, and the motion stays small.
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
      [attr.aria-busy]="inFlight()"
      [attr.aria-label]="inFlight() ? 'Sending…' : label()"
      [title]="inFlight() ? 'Sending…' : label()"
      (click)="send.emit($event)"
    >
      <span class="send-shimmer" aria-hidden="true"></span>
      <span class="send-plane-text">{{ text() }}</span>
      <span class="send-dots" aria-hidden="true"><i></i><i></i><i></i></span>
      <span class="send-sparkles" aria-hidden="true"><i></i><i></i><i></i><i></i><i></i><i></i></span>
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
  private readonly theme = inject(ThemeService);

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
  /** A theme's "it failed" wording shows for a moment after a failure. */
  private readonly failedNotice = signal(false);

  /** The visible text: the theme's words for the current moment, or "Send". */
  protected readonly text = computed(() => {
    const label = this.theme.text('send.label', 'Send');
    if (this.sendingVisible()) return this.theme.text('send.sending', label);
    if (this.failedNotice()) return this.theme.text('send.failed', label);
    return label;
  });

  private sendingTimer: ReturnType<typeof setTimeout> | null = null;
  private landingTimer: ReturnType<typeof setTimeout> | null = null;
  private failedTimer: ReturnType<typeof setTimeout> | null = null;

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
      this.land(response.status_code === 0 ? 'failed' : 'done');
    });

    inject(DestroyRef).onDestroy(() => {
      landed.unsubscribe();
      this.clearSendingTimer();
      if (this.landingTimer) clearTimeout(this.landingTimer);
      if (this.failedTimer) clearTimeout(this.failedTimer);
    });
  }

  private land(kind: Landing): void {
    if (this.failedTimer) clearTimeout(this.failedTimer);
    this.failedNotice.set(kind === 'failed');
    if (kind === 'failed') {
      this.failedTimer = setTimeout(() => this.failedNotice.set(false), FAILED_NOTICE_MS);
    }
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
