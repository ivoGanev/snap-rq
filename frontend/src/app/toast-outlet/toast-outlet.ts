import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { ToastService } from '../core/services/toast.service';

/**
 * Renders the single active toast, if any. Mounted once in the app shell.
 */
@Component({
  selector: 'app-toast-outlet',
  templateUrl: './toast-outlet.html',
  styleUrl: './toast-outlet.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ToastOutlet {
  protected readonly toasts = inject(ToastService);
}
