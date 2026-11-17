import { Injectable, signal } from '@angular/core';
import { SettingsService } from '../../../../bindings/snap-rq/backend/mcp';
import type { Status, ToolStatus } from '../../../../bindings/snap-rq/backend/mcp';

export type McpStatus = Status;
export type McpTool = ToolStatus;

const EMPTY_STATUS: McpStatus = { enabled: false, running: false, port: 0, url: '', error: '' };

/**
 * The app's typed API for the MCP server settings (Wails SettingsService).
 */
@Injectable({ providedIn: 'root' })
export class McpApiService {
  readonly status = signal<McpStatus>(EMPTY_STATUS);
  readonly tools = signal<McpTool[]>([]);

  async load(): Promise<void> {
    const [status, tools] = await Promise.all([SettingsService.Status(), SettingsService.Tools()]);
    this.status.set(status);
    this.tools.set(tools);
  }

  async setEnabled(enabled: boolean): Promise<void> {
    this.status.set(await SettingsService.SetEnabled(enabled));
  }

  async setPort(port: number): Promise<void> {
    this.status.set(await SettingsService.SetPort(port));
  }

  token(): Promise<string> {
    return SettingsService.Token();
  }

  rotateToken(): Promise<string> {
    return SettingsService.RotateToken();
  }

  async setToolEnabled(name: string, enabled: boolean): Promise<void> {
    await SettingsService.SetToolEnabled(name, enabled);
    this.tools.set(await SettingsService.Tools());
  }

  async resetTool(name: string): Promise<void> {
    await SettingsService.ResetTool(name);
    this.tools.set(await SettingsService.Tools());
  }

  async setRiskEnabled(risk: string, enabled: boolean): Promise<void> {
    await SettingsService.SetRiskEnabled(risk, enabled);
    this.tools.set(await SettingsService.Tools());
  }
}
