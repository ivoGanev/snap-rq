import { Component, computed, inject, input } from '@angular/core';
import { InspectorHeader } from '../../inspector-header/inspector-header';
import type { InspectorContentOf } from '../../inspector';
import { GroupApiService } from '../../../core/services/group.service';
import { RequestApiService } from '../../../core/services/request.service';
import type { HttpRequestSummary } from '../../../core/models/http';
import { requestColorInfo } from '../../../core/request-colors';

/** Inspector pane for a picked group: what is in it and how each request last went. */
@Component({
  selector: 'app-group-pane',
  imports: [InspectorHeader],
  templateUrl: './group-pane.html',
  styleUrl: './group-pane.scss',
})
export class GroupPane {
  private readonly groupApi = inject(GroupApiService);
  private readonly requestApi = inject(RequestApiService);

  readonly content = input.required<InspectorContentOf<'group'>>();

  protected readonly group = computed(() => this.groupApi.groups().find((g) => g.id === this.content().groupId) ?? null);

  protected readonly color = computed(() => requestColorInfo(this.group()?.color ?? null)?.cssVar ?? null);

  protected readonly requests = computed(() => {
    const group = this.group();
    if (!group) return [];
    return this.requestApi
      .forCollection(group.collection_id)
      .filter((r) => r.group_id === group.id)
      .sort((a, b) => a.user_sort_order - b.user_sort_order || a.name.localeCompare(b.name));
  });

  protected lastResult(req: HttpRequestSummary): string {
    if (req.response_id === 0) return 'not sent';
    if (req.status_code === 0) return 'error';
    return String(req.status_code);
  }
}
