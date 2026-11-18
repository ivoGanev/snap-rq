import { Injectable, computed, signal } from '@angular/core';
import { Events } from '@wailsio/runtime';
import { SettingsService } from '../../../../bindings/snap-rq/backend/mcp';
import type { ApprovalRequest } from '../../../../bindings/snap-rq/backend/mcp';

export interface McpApproval {
  id: string;
  title: string;
  lines: string[];
  warnings: string[];
  expiresAt: number;
}

const toApproval = (r: ApprovalRequest): McpApproval => ({
  id: r.id,
  title: r.title,
  lines: r.lines ?? [],
  warnings: r.warnings ?? [],
  expiresAt: r.expires_at,
});

/**
 * Requests from AI clients that wait for the user's answer. The backend sends
 * 'mcp:approval' when a risky call arrives and 'mcp:approval-closed' when it
 * is answered or runs out of time. The oldest is shown first.
 */
@Injectable({ providedIn: 'root' })
export class McpApprovalService {
  private readonly queue = signal<McpApproval[]>([]);

  readonly current = computed(() => this.queue()[0] ?? null);
  readonly waiting = computed(() => Math.max(0, this.queue().length - 1));

  constructor() {
    Events.On('mcp:approval', (ev) => this.add(toApproval(ev.data as ApprovalRequest)));
    Events.On('mcp:approval-closed', (ev) => this.remove(ev.data as string));
    void this.loadPending();
  }

  /** A reloaded window picks up requests that are still waiting. */
  async loadPending(): Promise<void> {
    try {
      for (const r of (await SettingsService.PendingApprovals()) ?? []) {
        this.add(toApproval(r));
      }
    } catch (err) {
      console.error('Failed to load pending approvals:', err);
    }
  }

  async respond(id: string, allow: boolean): Promise<void> {
    this.remove(id);
    try {
      await SettingsService.RespondApproval(id, allow);
    } catch (err) {
      console.error('Failed to answer an approval:', err);
    }
  }

  private add(approval: McpApproval): void {
    this.queue.update((q) =>
      q.some((a) => a.id === approval.id)
        ? q
        : [...q, approval].sort((a, b) => a.expiresAt - b.expiresAt),
    );
  }

  private remove(id: string): void {
    this.queue.update((q) => q.filter((a) => a.id !== id));
  }
}
