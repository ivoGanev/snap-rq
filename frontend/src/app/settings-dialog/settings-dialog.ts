import { Component, input, OnInit, output, signal } from '@angular/core';
import { McpSettings } from '../mcp-settings/mcp-settings';
import { SettingsGeneral } from '../settings-general/settings-general';
import { SettingsSecurity } from '../settings-security/settings-security';
import { SettingsBackups } from '../settings-backups/settings-backups';
import { SettingsUpdates } from '../settings-updates/settings-updates';

export type SettingsCategory = 'general' | 'security' | 'backups' | 'mcp' | 'updates';

/** One settings window: categories on the left, the chosen one on the right. */
@Component({
  selector: 'app-settings-dialog',
  imports: [SettingsGeneral, SettingsSecurity, SettingsBackups, McpSettings, SettingsUpdates],
  templateUrl: './settings-dialog.html',
  styleUrl: './settings-dialog.scss',
})
export class SettingsDialog implements OnInit {
  readonly initial = input<SettingsCategory>('general');
  readonly close = output<void>();

  protected readonly categories: { id: SettingsCategory; label: string }[] = [
    { id: 'general', label: 'General' },
    { id: 'security', label: 'Security' },
    { id: 'backups', label: 'Backups' },
    { id: 'mcp', label: 'MCP server' },
    { id: 'updates', label: 'Updates' },
  ];
  protected readonly category = signal<SettingsCategory>('general');

  ngOnInit(): void {
    this.category.set(this.initial());
  }
}
