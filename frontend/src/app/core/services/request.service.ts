import { Injectable, signal } from '@angular/core';
import * as RequestService from '../../../../bindings/snap-rq/backend/services';
import type { HttpRequest, HttpResponse } from '../../../../bindings/snap-rq/backend/models';

export type { HttpRequest, HttpResponse };

/**
 * Angular wrapper around the Wails-generated RequestService bindings.
 */
@Injectable({ providedIn: 'root' })
export class RequestApiService {
  readonly requests = signal<HttpRequest[]>([]);
  readonly responses = signal<HttpResponse[]>([]);

  async create(req: Omit<HttpRequest, 'id'>): Promise<HttpRequest> {
    const created = await RequestService.RequestService.CreateRequest(req as HttpRequest);
    await this.loadAll();
    return created;
  }

  async get(id: number): Promise<HttpRequest> {
    return RequestService.RequestService.GetRequest(id);
  }

  async loadAll(): Promise<void> {
    const all = await RequestService.RequestService.GetAllRequests();
    this.requests.set(all ?? []);
  }

  async loadForCollection(collectionId: number): Promise<void> {
    const all = await RequestService.RequestService.GetRequestsForCollection(collectionId);
    this.requests.set(all ?? []);
  }

  async update(req: HttpRequest): Promise<HttpRequest> {
    return RequestService.RequestService.UpdateRequest(req);
  }

  async delete(id: number): Promise<void> {
    await RequestService.RequestService.DeleteRequest(id);
    await this.loadAll();
  }

  async loadResponsesForRequest(requestId: number): Promise<void> {
    const all = await RequestService.RequestService.GetResponsesForRequest(requestId);
    this.responses.set(all ?? []);
  }

  async createResponse(resp: Omit<HttpResponse, 'id'>): Promise<HttpResponse> {
    return RequestService.RequestService.CreateResponse(resp as HttpResponse);
  }

  async execute(requestId: number, environmentId: number): Promise<HttpResponse> {
    return RequestService.RequestService.ExecuteRequest(requestId, environmentId);
  }
}
