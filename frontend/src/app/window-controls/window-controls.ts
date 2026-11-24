import { Component, DestroyRef, inject, signal } from '@angular/core';
import { Window } from '@wailsio/runtime';

/**
 * Minimise, maximise/restore and close for the frameless Windows window,
 * drawn to match the system's own. The top bar around them drags the window.
 */
@Component({
  selector: 'app-window-controls',
  templateUrl: './window-controls.html',
  styleUrl: './window-controls.scss',
})
export class WindowControls {
  protected readonly maximised = signal(false);

  constructor() {
    // Maximising, restoring and snapping all resize the page.
    const sync = () => void Window.IsMaximised().then((value) => this.maximised.set(value), () => {});
    sync();
    window.addEventListener('resize', sync);
    inject(DestroyRef).onDestroy(() => window.removeEventListener('resize', sync));
  }

  protected minimise(): void {
    void Window.Minimise();
  }

  protected toggleMaximise(): void {
    void Window.ToggleMaximise();
  }

  protected close(): void {
    void Window.Close();
  }
}
