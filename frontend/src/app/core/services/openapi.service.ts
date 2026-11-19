import { Injectable, inject } from '@angular/core';
import { CommandApiService } from './command.service';
import { CollectionApiService } from './collection.service';
import { RequestApiService } from './request.service';

export interface OpenApiImportResult {
  collections_created: number;
  requests_created: number;
  requests_updated: number;
}

export interface OpenApiFieldChange {
  field: 'query' | 'headers' | 'body type' | 'body';
  before: string;
  after: string;
}

/** One request the file describes, compared with the collection in the project. */
export interface OpenApiPreviewRequest {
  key: string;
  method: string;
  path: string;
  status: 'new' | 'changed' | 'unchanged';
  changes: OpenApiFieldChange[];
}

/** One collection the file describes. `exists` means the project already has that name. */
export interface OpenApiPreviewCollection {
  name: string;
  exists: boolean;
  new: number;
  changed: number;
  unchanged: number;
  requests: OpenApiPreviewRequest[];
  /** Requests the project has that the file does not (kept as they are). */
  not_in_spec: string[];
}

/** What the user ticked in one collection: request keys to add and to update. */
export interface OpenApiPick {
  name: string;
  add: string[];
  update: string[];
}

export interface OpenApiExportResult {
  collections_exported: number;
  requests_exported: number;
  requests_skipped: number;
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

  async previewSpec(projectId: number, filePath: string): Promise<OpenApiPreviewCollection[]> {
    const result = await this.commands.execute<{ collections: OpenApiPreviewCollection[] }>(
      'openapi.preview',
      { project_id: projectId, file_path: filePath },
    );
    return result.collections;
  }

  async importSpec(
    projectId: number,
    filePath: string,
    collections: OpenApiPick[],
  ): Promise<OpenApiImportResult> {
    const result = await this.commands.execute<OpenApiImportResult>('openapi.import', {
      project_id: projectId,
      file_path: filePath,
      collections,
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
