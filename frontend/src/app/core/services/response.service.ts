import { Injectable, inject } from '@angular/core';
import type { HttpRequest, HttpResponse } from '../models/http';
import type { EnvironmentContext } from '../models/environment-context';
import { CommandApiService } from './command.service';
import { RequestApiService } from './request.service';

export type { HttpRequest, HttpResponse };

export interface SendRequestCallbacks {
  onPending?: (resp: HttpResponse) => void;
  onUpdate?: (resp: HttpResponse) => void;
}

/**
 * Response persistence and asynchronous request execution tracking, all
 * through commands: the send is a lock-free external call, the pending
 * response and its in-flight duration are rows updated as the send runs,
 * and the final outcome is recorded on the request row.
 */
@Injectable({ providedIn: 'root' })
export class ResponseApiService {
  private readonly commands = inject(CommandApiService);
  private readonly requestApi = inject(RequestApiService);

  async create(resp: Omit<HttpResponse, 'id'>): Promise<HttpResponse> {
    return this.commands.execute('response.save', resp);
  }

  async update(resp: HttpResponse): Promise<HttpResponse> {
    return this.commands.execute('response.update', resp);
  }

  async delete(id: number): Promise<void> {
    // Response history is append-only and capped; removing single entries
    // by hand is not an operation the app offers through commands yet.
    throw new Error('deleting response history entries is not supported');
  }

  /**
   * Sends a request, creates a pending response record, updates its duration
   * while the request is in flight, and writes the final response data when
   * complete. Multiple calls run independently.
   *
   * The request is interpolated against `environment` for execution, while
   * the raw (uninterpolated) request is what gets persisted afterwards.
   */
  async sendRequest(
    req: HttpRequest,
    environment: EnvironmentContext,
    callbacks?: SendRequestCallbacks,
    trackUsage = true,
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
      const rawResponse = await this.requestApi.execute(environment.interpolateRequest(req));
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

      // The send flow's owned write: status/response columns only. User
      // edits (request.update) never touch these columns, so an undo can
      // never clobber a send's result.
      await this.requestApi.updateRequestResponse(
        req.id,
        rawResponse.status_code,
        currentResponse.id,
      );

      this.requestApi.responses.update(current =>
        current.map(r => (r.id === currentResponse.id ? currentResponse : r)),
      );
      callbacks?.onUpdate?.(currentResponse);

      if (trackUsage) {
        try {
          await this.requestApi.recordRequestUsage([req.id]);
        } catch (err) {
          console.error('Failed to record request usage:', err);
        }
      }

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

  /**
   * Sends a batch of requests concurrently (HTTP only, no database writes
   * during flight) and persists every response plus request status update
   * in one command run.
   */
  async sendRequests(
    reqs: HttpRequest[],
    environment: EnvironmentContext,
    trackUsage = true,
  ): Promise<HttpResponse[]> {
    if (reqs.length === 0) return [];

    const createdAt = new Date().toISOString().replace('T', ' ').slice(0, 19);
    const raw = await Promise.all(
      reqs.map((req) => this.requestApi.execute(environment.interpolateRequest(req))),
    );
    const responses = raw.map((resp) =>
      resp.created_at ? resp : { ...resp, created_at: createdAt },
    );

    const saved = await this.requestApi.bulkSaveResponses(responses);
    if (saved.length > 0) {
      this.requestApi.responses.update((current) => [...saved, ...current]);
    }

    if (trackUsage && saved.length > 0) {
      try {
        await this.requestApi.recordRequestUsage(saved.map((resp) => resp.request_id));
      } catch (err) {
        console.error('Failed to record request usage:', err);
      }
    }

    return saved;
  }
}
