import { Injectable, inject } from '@angular/core';
import { CommandApiService } from './command.service';
import { CollectionApiService } from './collection.service';
import { RequestApiService } from './request.service';

export interface OpenApiImportResult {
  collections_created: number;
  requests_created: number;
}

export interface OpenApiExportResult {
  collections_exported: number;
  requests_exported: number;
}

/**
 * Thin wrapper over the openapi.import / openapi.export commands. Import
 * reloads the collection and request stores because a bulk load creates many
 * rows in one command.
 */
@Injectable({ providedIn: 'root' })
export class OpenApiApiService {
  private readonly commands = inject(CommandApiService);
  private readonly collectionApi = inject(CollectionApiService);
  private readonly requestApi = inject(RequestApiService);

  async importSpec(projectId: number, filePath: string): Promise<OpenApiImportResult> {
    const result = await this.commands.execute<OpenApiImportResult>('openapi.import', {
      project_id: projectId,
      file_path: filePath,
    });
    await Promise.all([
      this.collectionApi.loadForProject(projectId),
      this.requestApi.loadAll(),
    ]);
    return result;
  }

  async exportSpec(projectId: number, filePath: string): Promise<OpenApiExportResult> {
    return this.commands.execute<OpenApiExportResult>('openapi.export', {
      project_id: projectId,
      file_path: filePath,
    });
  }
}
