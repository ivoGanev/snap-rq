import { Component, computed, inject, OnDestroy, OnInit, signal } from '@angular/core';
import { ConfirmService } from '../core/services/confirm.service';
import { McpApiService, type McpTool } from '../core/services/mcp.service';

interface RiskGroup {
  risk: string;
  label: string;
  tools: McpTool[];
  allOn: boolean;
}

const RISK_LABELS: Record<string, string> = {
  read: 'Look — see your projects, collections and requests',
  organize: 'Tidy up — rename, move, tag, colour and reorder. Deleted requests go to the trash',
  content:
    'Edit requests — change where a request goes or what it carries: its address (URL), headers and body',
};

/**
 * Settings for the built-in MCP server: switch it on, pick the port, manage the
 * token, and choose which tools AI clients may use.
 */
@Component({
  selector: 'app-mcp-settings',
  templateUrl: './mcp-settings.html',
  styleUrl: './mcp-settings.scss',
})
export class McpSettings implements OnInit, OnDestroy {
  protected readonly api = inject(McpApiService);
  private readonly confirm = inject(ConfirmService);

  protected readonly error = signal('');
  protected readonly token = signal('');
  protected readonly tokenVisible = signal(false);
  protected readonly copied = signal('');

  protected readonly groups = computed<RiskGroup[]>(() => {
    const byRisk = new Map<string, McpTool[]>();
    for (const tool of this.api.tools()) {
      byRisk.set(tool.risk, [...(byRisk.get(tool.risk) ?? []), tool]);
    }
    return [...byRisk.entries()].map(([risk, tools]) => ({
      risk,
      label: RISK_LABELS[risk] ?? risk,
      tools,
      allOn: tools.every((t) => t.enabled),
    }));
  });

  private refreshTimer: ReturnType<typeof setInterval> | undefined;

  ngOnInit(): void {
    void this.run(async () => {
      await this.api.load();
      this.token.set(await this.api.token());
      await this.api.loadActions();
    });
    this.refreshTimer = setInterval(() => void this.api.loadActions().catch(() => undefined), 3000);
  }

  ngOnDestroy(): void {
    clearInterval(this.refreshTimer);
  }

  protected clearActions(): void {
    void this.run(() => this.api.clearActions());
  }

  protected timeLabel(at: number): string {
    return new Date(at).toLocaleString();
  }

  protected async run(action: () => Promise<void>): Promise<void> {
    this.error.set('');
    try {
      await action();
    } catch (err) {
      this.error.set(err instanceof Error ? err.message : String(err));
    }
  }

  protected toggleEnabled(event: Event): void {
    const checked = (event.target as HTMLInputElement).checked;
    void this.run(() => this.api.setEnabled(checked));
  }

  protected changePort(event: Event): void {
    const input = event.target as HTMLInputElement;
    const port = Math.trunc(Number(input.value));
    if (port === this.api.status().port) return;
    void this.run(async () => {
      try {
        await this.api.setPort(port);
      } finally {
        input.value = String(this.api.status().port);
      }
    });
  }

  protected rotate(): void {
    void this.run(async () => {
      this.token.set(await this.api.rotateToken());
    });
  }

  protected async toggleApproval(event: Event): Promise<void> {
    const input = event.target as HTMLInputElement;
    const on = input.checked;
    if (!on) {
      const ok = await this.confirm.ask({
        title: 'Stop asking before changes?',
        message:
          'The AI will be able to rename, move, change and delete your requests without asking you first. Changing an address could send your saved logins to another server.',
        confirmLabel: 'Stop asking',
      });
      if (!ok) {
        input.checked = true;
        return;
      }
    }
    await this.run(() => this.api.setRequireApproval(on));
    input.checked = this.api.requireApproval();
  }

  protected async toggleMasking(event: Event): Promise<void> {
    const input = event.target as HTMLInputElement;
    const enabled = input.checked;
    if (!enabled) {
      const ok = await this.confirm.ask({
        title: 'Show secrets to AI?',
        message:
          'With masking off, AI clients can read tokens, passwords and API keys stored in your request URLs, headers and bodies. Turn it off only if you trust every client you connect.',
        confirmLabel: 'Turn off masking',
      });
      if (!ok) {
        input.checked = true;
        return;
      }
    }
    await this.run(() => this.api.setMasking(enabled, this.api.masking().extra ?? []));
    input.checked = this.api.masking().enabled;
  }

  protected saveMaskNames(event: Event): void {
    const names = (event.target as HTMLTextAreaElement).value.split('\n');
    void this.run(() => this.api.setMasking(this.api.masking().enabled, names));
  }

  protected builtinNames(): string[] {
    return this.api.masking().builtin ?? [];
  }

  protected maskNamesText(): string {
    return (this.api.masking().extra ?? []).join('\n');
  }

  protected toggleTool(tool: McpTool, event: Event): void {
    const checked = (event.target as HTMLInputElement).checked;
    void this.run(() => this.api.setToolEnabled(tool.name, checked));
  }

  protected toggleRisk(group: RiskGroup, event: Event): void {
    const checked = (event.target as HTMLInputElement).checked;
    void this.run(() => this.api.setRiskEnabled(group.risk, checked));
  }

  protected reset(tool: McpTool): void {
    void this.run(() => this.api.resetTool(tool.name));
  }

  protected async copy(label: string, text: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(text);
      this.copied.set(label);
      setTimeout(() => this.copied.set(''), 1500);
    } catch (err) {
      console.error('Copy failed:', err);
    }
  }

  protected maskedToken(): string {
    return this.tokenVisible() ? this.token() : '•'.repeat(24);
  }
}
