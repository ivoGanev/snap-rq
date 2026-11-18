import { Component, HostListener, OnDestroy, computed, inject, signal } from '@angular/core';
import { McpApprovalService } from '../core/services/mcp-approval.service';

/**
 * The dialog that asks the user to approve a risky change an AI client wants
 * to make. Mounted once in the app shell. Escape denies.
 */
@Component({
  selector: 'app-mcp-approval-outlet',
  templateUrl: './mcp-approval-outlet.html',
  styleUrl: './mcp-approval-outlet.scss',
})
export class McpApprovalOutlet implements OnDestroy {
  protected readonly approvals = inject(McpApprovalService);

  private readonly now = signal(Date.now());
  private readonly timer = setInterval(() => this.now.set(Date.now()), 1000);

  protected readonly secondsLeft = computed(() => {
    const current = this.approvals.current();
    return current ? Math.max(0, Math.ceil((current.expiresAt - this.now()) / 1000)) : 0;
  });

  ngOnDestroy(): void {
    clearInterval(this.timer);
  }

  @HostListener('document:keydown', ['$event'])
  onKeydown(event: KeyboardEvent): void {
    const current = this.approvals.current();
    if (!current) return;
    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      void this.approvals.respond(current.id, false);
    }
  }

  protected answer(id: string, allow: boolean): void {
    void this.approvals.respond(id, allow);
  }
}
