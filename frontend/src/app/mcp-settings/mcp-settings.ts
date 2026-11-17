import { Component, computed, inject, OnInit, output, signal } from '@angular/core';
import { McpApiService, type McpTool } from '../core/services/mcp.service';

interface RiskGroup {
  risk: string;
  label: string;
  tools: McpTool[];
  allOn: boolean;
}

const RISK_LABELS: Record<string, string> = {
  read: 'Read — look at projects, collections and requests',
  organize: 'Organize — create, rename, move, tag, reorder',
  destructive: 'Destructive — delete',
  external: 'External — send requests',
  sensitive: 'Sensitive — secrets and files',
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
export class McpSettings implements OnInit {
  protected readonly api = inject(McpApiService);

  readonly close = output<void>();

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

  protected readonly snippets = computed(() => {
    const url = this.api.status().url || `http://127.0.0.1:${this.api.status().port}/mcp`;
    const token = this.token() || '<token>';
    return [
      {
        label: 'Claude Code',
        text: `claude mcp add --transport http snap-rq ${url} --header "Authorization: Bearer ${token}"`,
      },
      {
        label: 'JSON config',
        text: JSON.stringify(
          { mcpServers: { 'snap-rq': { type: 'http', url, headers: { Authorization: `Bearer ${token}` } } } },
          null,
          2,
        ),
      },
    ];
  });

  ngOnInit(): void {
    void this.run(async () => {
      await this.api.load();
      this.token.set(await this.api.token());
    });
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

  protected closeDialog(): void {
    this.close.emit();
  }
}
