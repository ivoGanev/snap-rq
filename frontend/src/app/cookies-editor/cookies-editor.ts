import { Component, computed, inject, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { CookieApiService, type Cookie } from '../core/services/cookie.service';

interface DomainGroup {
  domain: string;
  cookies: Cookie[];
}

/**
 * Manager for the current project's cookie jar: view, edit, add and delete cookies.
 */
@Component({
  selector: 'app-cookies-editor',
  imports: [FormsModule],
  templateUrl: './cookies-editor.html',
  styleUrl: './cookies-editor.scss',
})
export class CookiesEditor {
  protected readonly state = inject(WorkspaceStateService);
  private readonly cookieApi = inject(CookieApiService);

  readonly close = output<void>();

  protected readonly cookies = this.cookieApi.cookies;

  protected readonly groups = computed<DomainGroup[]>(() => {
    const map = new Map<string, Cookie[]>();
    for (const cookie of this.cookies()) {
      const list = map.get(cookie.domain) ?? [];
      list.push(cookie);
      map.set(cookie.domain, list);
    }
    return [...map.entries()].map(([domain, cookies]) => ({ domain, cookies }));
  });

  protected readonly addOpen = signal(false);
  protected readonly error = signal('');
  protected readonly draft = signal(this.emptyDraft());

  private emptyDraft() {
    return {
      domain: '',
      path: '/',
      name: '',
      value: '',
      expires: '',
      subdomains: false,
      secure: false,
      http_only: false,
    };
  }

  protected setDraft<K extends keyof ReturnType<CookiesEditor['emptyDraft']>>(
    key: K,
    value: ReturnType<CookiesEditor['emptyDraft']>[K],
  ): void {
    this.draft.update((d) => ({ ...d, [key]: value }));
  }

  protected openAdd(): void {
    this.draft.set(this.emptyDraft());
    this.error.set('');
    this.addOpen.set(true);
  }

  protected expiryLabel(cookie: Cookie): string {
    return cookie.expires_at > 0 ? new Date(cookie.expires_at * 1000).toLocaleString() : 'Session';
  }

  protected async submitAdd(): Promise<void> {
    const project = this.state.selectedProject();
    if (!project) return;
    const d = this.draft();
    let expiresAt = 0;
    if (d.expires) {
      const ms = new Date(d.expires).getTime();
      if (Number.isNaN(ms)) {
        this.error.set('Invalid expiry date.');
        return;
      }
      expiresAt = Math.floor(ms / 1000);
    }
    try {
      await this.cookieApi.save({
        project_id: project.id,
        domain: d.domain.trim(),
        host_only: !d.subdomains,
        path: d.path.trim() || '/',
        name: d.name.trim(),
        value: d.value,
        expires_at: expiresAt,
        secure: d.secure,
        http_only: d.http_only,
        same_site: '',
      });
      this.addOpen.set(false);
      this.error.set('');
    } catch (err) {
      this.error.set(err instanceof Error ? err.message : String(err));
    }
  }

  protected async updateValue(cookie: Cookie, value: string): Promise<void> {
    if (value === cookie.value) return;
    try {
      await this.cookieApi.save({ ...cookie, value });
    } catch (err) {
      console.error(err);
    }
  }

  protected async deleteCookie(cookie: Cookie): Promise<void> {
    try {
      await this.cookieApi.delete(cookie.id);
    } catch (err) {
      console.error(err);
    }
  }

  protected async clearDomain(domain: string): Promise<void> {
    const project = this.state.selectedProject();
    if (!project) return;
    try {
      await this.cookieApi.clear(project.id, domain);
    } catch (err) {
      console.error(err);
    }
  }

  protected async clearAll(): Promise<void> {
    const project = this.state.selectedProject();
    if (!project) return;
    try {
      await this.cookieApi.clear(project.id);
    } catch (err) {
      console.error(err);
    }
  }

  protected closeDialog(): void {
    this.close.emit();
  }
}
