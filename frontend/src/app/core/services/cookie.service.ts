import { Injectable, inject, signal } from '@angular/core';
import { CommandApiService } from './command.service';
import { ChangeBus } from './change-bus.service';

export interface Cookie {
  id: number;
  project_id: number;
  domain: string;
  host_only: boolean;
  path: string;
  name: string;
  value: string;
  /** Unix seconds; 0 means a session cookie. */
  expires_at: number;
  secure: boolean;
  http_only: boolean;
  same_site: string;
  created_at: number;
}

export type CookieInput = Omit<Cookie, 'id' | 'created_at'> & { id?: number };

/**
 * The app's typed API for the per-project cookie jar.
 */
@Injectable({ providedIn: 'root' })
export class CookieApiService {
  private readonly commands = inject(CommandApiService);
  private readonly bus = inject(ChangeBus);
  private projectId: number | null = null;

  readonly cookies = signal<Cookie[]>([]);

  constructor() {
    this.bus.changesFor('cookie').subscribe(() => void this.reload());
  }

  async loadForProject(projectId: number | null): Promise<void> {
    this.projectId = projectId;
    await this.reload();
  }

  /** Re-reads the jar; also used after a send, since jar writes emit no change events. */
  async reload(): Promise<void> {
    if (!this.projectId) {
      this.cookies.set([]);
      return;
    }
    const all = await this.commands.execute<Cookie[]>('cookie.list', { project_id: this.projectId });
    this.cookies.set(all ?? []);
  }

  async save(cookie: CookieInput): Promise<Cookie> {
    return this.commands.execute<Cookie>('cookie.save', { ...cookie, id: cookie.id ?? 0 });
  }

  async delete(id: number): Promise<void> {
    await this.commands.execute('cookie.delete', { id });
  }

  async clear(projectId: number, domain?: string): Promise<void> {
    await this.commands.execute('cookie.clear', { project_id: projectId, domain });
  }
}
