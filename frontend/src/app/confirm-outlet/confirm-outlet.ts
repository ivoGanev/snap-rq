import { ChangeDetectionStrategy, Component, HostListener, inject } from '@angular/core';
import { ConfirmService } from '../core/services/confirm.service';

/**
 * Renders the single active confirmation dialog. Mounted once in the app shell.
 * While a dialog is open it swallows Escape (cancels) and Delete so the
 * underlying list/sidebar handlers never act behind the modal.
 */
@Component({
  selector: 'app-confirm-outlet',
  templateUrl: './confirm-outlet.html',
  styleUrl: './confirm-outlet.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ConfirmOutlet {
  protected readonly confirm = inject(ConfirmService);

  @HostListener('document:keydown', ['$event'])
  onKeydown(event: KeyboardEvent): void {
    if (!this.confirm.active()) return;
    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      this.confirm.respond(false);
      return;
    }
    if (event.key === 'Delete') {
      event.stopPropagation();
    }
  }
}
