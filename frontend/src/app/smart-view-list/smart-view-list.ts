import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';
import { SMART_VIEWS, type SmartViewId } from '../request-groups/sidebar-views';

@Component({
  selector: 'app-smart-view-list',
  templateUrl: './smart-view-list.html',
  styleUrl: './smart-view-list.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SmartViewList {
  readonly views = SMART_VIEWS;
  readonly activeView = input<SmartViewId | null>(null);
  readonly disabled = input(false);
  readonly viewSelect = output<SmartViewId>();
}
