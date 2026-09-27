import { Injectable, inject } from '@angular/core';
import * as RequestService from '../../../../bindings/snap-rq/backend/services';
import { RequestApiService, type HttpRequest, type HttpResponse } from './request.service';

export type { HttpRequest, HttpResponse };

export interface SendRequestCallbacks {
  onPending?: (resp: HttpResponse) => void;
  onUpdate?: (resp: HttpResponse) => void;
}

/**
 * Angular abstraction around response persistence and asynchronous request
 * execution tracking. Each send creates a response record immediately so the
 * duration can be updated in the database while the request is in flight, and
 * so multiple concurrent requests are tracked independently.
 */
@Injectable({ providedIn: 'root' })
export class ResponseApiService {
  private readonly requestApi = inject(RequestApiService);

  async create(resp: Omit<HttpResponse, 'id'>): Promise<HttpResponse> {
    return RequestService.RequestService.CreateResponse(resp as HttpResponse);
  }

  async update(resp: HttpResponse): Promise<HttpResponse> {
    return RequestService.RequestService.UpdateResponse(resp);
  }

  async delete(id: number): Promise<void> {
    await RequestService.RequestService.DeleteResponse(id);
  }

  /**
   * Sends a request, creates a pending response record, updates its duration
   * in the database while the request is in flight, and writes the final
   * response data when complete. Multiple calls run independently.
   */
  async sendRequest(
    req: HttpRequest,
    environmentId: number,
    callbacks?: SendRequestCallbacks,
  ): Promise<HttpResponse> {
    const start = Date.now();
    const createdAt = new Date().toISOString().replace('T', ' ').slice(0, 19);

    const pending = await this.create({
      request_id: req.id,
      headers: '',
      status_code: 0,
      body: 'Sending...',
      created_at: createdAt,
      duration_ms: 0,
    } as Omit<HttpResponse, 'id'>);

    this.requestApi.responses.update(current => [pending, ...current]);
    callbacks?.onPending?.(pending);

    let currentResponse = pending;
    let completed = false;

    const updateDuration = async (elapsed: number): Promise<void> => {
      if (completed) {
        return;
      }
      currentResponse = await this.update({ ...currentResponse, duration_ms: elapsed });
      this.requestApi.responses.update(current =>
        current.map(r => (r.id === pending.id ? currentResponse : r)),
      );
      callbacks?.onUpdate?.(currentResponse);
    };

    const intervalId = window.setInterval(() => {
      const elapsed = Date.now() - start;
      updateDuration(elapsed).catch(err => {
        console.error('Failed to update response duration:', err);
      });
    }, 50);

    try {
      const rawResponse = await this.requestApi.execute(req.id, environmentId);
      completed = true;
      window.clearInterval(intervalId);
      const totalDuration = Date.now() - start;

      currentResponse = await this.update({
        ...currentResponse,
        headers: rawResponse.headers,
        status_code: rawResponse.status_code,
        body: rawResponse.body,
        duration_ms: totalDuration,
      });

      await this.requestApi.update({
        ...req,
        status_code: rawResponse.status_code,
        response_id: currentResponse.id,
      });

      this.requestApi.responses.update(current =>
        current.map(r => (r.id === currentResponse.id ? currentResponse : r)),
      );
      callbacks?.onUpdate?.(currentResponse);
      return currentResponse;
    } catch (err) {
      completed = true;
      window.clearInterval(intervalId);
      const errorMessage = err instanceof Error ? err.message : 'Request failed';
      currentResponse = await this.update({
        ...currentResponse,
        status_code: 0,
        body: errorMessage,
        duration_ms: Date.now() - start,
      });

      this.requestApi.responses.update(current =>
        current.map(r => (r.id === currentResponse.id ? currentResponse : r)),
      );
      callbacks?.onUpdate?.(currentResponse);
      throw err;
    } finally {
      window.clearInterval(intervalId);
    }
  }
}
