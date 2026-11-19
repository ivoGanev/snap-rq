import type { Type } from '@angular/core';
import type { InspectorContentKind } from './inspector-content';
import { MessagePane } from './panes/message-pane/message-pane';
import { ResponsePane } from './panes/response-pane/response-pane';
import { BinnedRequestPane } from './panes/binned-request-pane/binned-request-pane';

/**
 * The pane component for each kind of Inspector content. Every pane takes the
 * matching content member as its required `content` input.
 */
export const INSPECTOR_PANES: { readonly [K in InspectorContentKind]: Type<unknown> } = {
  message: MessagePane,
  response: ResponsePane,
  'binned-request': BinnedRequestPane,
};
