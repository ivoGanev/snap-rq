/**
 * What the Inspector (the right-hand panel) is showing.
 *
 * To add a new kind of content: add a member here, write a pane component that
 * takes it as its `content` input, and register the pane in `INSPECTOR_PANES`
 * (inspector-panes.ts). The pane map is typed against this union, so a kind
 * without a pane does not compile.
 */
export type InspectorContent =
  /** A plain message, used for empty and multi-selection states. */
  | { kind: 'message'; text: string }
  /** The responses of a live request. */
  | { kind: 'response'; requestId: number }
  /** A request in the trash bin. */
  | { kind: 'binned-request'; binId: number };

export type InspectorContentKind = InspectorContent['kind'];

/** The content member for one kind, e.g. `InspectorContentOf<'response'>`. */
export type InspectorContentOf<K extends InspectorContentKind> = Extract<InspectorContent, { kind: K }>;
