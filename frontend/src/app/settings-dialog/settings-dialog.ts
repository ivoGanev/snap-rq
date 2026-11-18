import { Component, input, OnInit, output, signal } from '@angular/core';
import { McpSettings } from '../mcp-settings/mcp-settings';
import { SettingsGeneral } from '../settings-general/settings-general';
import { SettingsUpdates } from '../settings-updates/settings-updates';

export type SettingsCategory = 'general' | 'mcp' | 'updates';

/** One settings window: categories on the left, the chosen one on the right. */
@Component({
  selector: 'app-settings-dialog',
  imports: [SettingsGeneral, McpSettings, SettingsUpdates],
  templateUrl: './settings-dialog.html',
  styleUrl: './settings-dialog.scss',
})
export class SettingsDialog implements OnInit {
  readonly initial = input<SettingsCategory>('general');
  readonly close = output<void>();

  protected readonly categories: { id: SettingsCategory; label: string }[] = [
    { id: 'general', label: 'General' },
    { id: 'mcp', label: 'MCP server' },
    { id: 'updates', label: 'Updates' },
  ];
  protected readonly category = signal<SettingsCategory>('general');

  ngOnInit(): void {
    this.category.set(this.initial());
  }
}
